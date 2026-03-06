package nodeloom

import (
	"context"
	"log"
	"sync"
	"time"
)

// batchProcessor reads events from a queue, accumulates them into batches,
// and flushes them via the transport. It flushes when the batch reaches
// batchSize or when the flush interval elapses, whichever comes first.
type batchProcessor struct {
	queue     *queue
	transport *transport
	batchSize int
	interval  time.Duration

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// newBatchProcessor creates a batch processor that reads from the given queue
// and sends events using the provided transport.
func newBatchProcessor(q *queue, t *transport, batchSize int, interval time.Duration) *batchProcessor {
	return &batchProcessor{
		queue:     q,
		transport: t,
		batchSize: batchSize,
		interval:  interval,
		stopCh:    make(chan struct{}),
	}
}

// start begins the background goroutine that processes queued events.
func (bp *batchProcessor) start() {
	bp.wg.Add(1)
	go bp.run()
}

// run is the main processing loop. It collects events into a buffer and
// flushes when the batch is full or the ticker fires.
func (bp *batchProcessor) run() {
	defer bp.wg.Done()

	ticker := time.NewTicker(bp.interval)
	defer ticker.Stop()

	buffer := make([]*TelemetryEvent, 0, bp.batchSize)

	for {
		select {
		case event, ok := <-bp.queue.channel():
			if !ok {
				// Channel closed during shutdown. Flush remaining events.
				if len(buffer) > 0 {
					bp.flush(buffer)
				}
				return
			}
			buffer = append(buffer, event)
			if len(buffer) >= bp.batchSize {
				bp.flush(buffer)
				buffer = make([]*TelemetryEvent, 0, bp.batchSize)
			}

		case <-ticker.C:
			if len(buffer) > 0 {
				bp.flush(buffer)
				buffer = make([]*TelemetryEvent, 0, bp.batchSize)
			}

		case <-bp.stopCh:
			// Drain any remaining events from the queue.
			remaining := bp.queue.drain(bp.batchSize * 10)
			buffer = append(buffer, remaining...)
			if len(buffer) > 0 {
				bp.flush(buffer)
			}
			return
		}
	}
}

// flush sends a batch of events via the transport. Errors are logged
// but not propagated, consistent with the fire-and-forget design.
func (bp *batchProcessor) flush(events []*TelemetryEvent) {
	if len(events) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := bp.transport.sendBatch(ctx, events); err != nil {
		log.Printf("[nodeloom] failed to flush batch of %d events: %v", len(events), err)
	}
}

// stop signals the processor to stop and waits for it to finish.
// It blocks until all buffered events have been flushed or the timeout expires.
func (bp *batchProcessor) stop(timeout time.Duration) {
	close(bp.stopCh)

	done := make(chan struct{})
	go func() {
		bp.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(timeout):
		log.Printf("[nodeloom] shutdown timed out after %v, some events may be lost", timeout)
	}
}
