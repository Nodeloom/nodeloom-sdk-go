package nodeloom

import "log"

// queue is a bounded, channel-based event queue. It is safe for concurrent use
// because Go channels handle synchronization internally.
type queue struct {
	ch chan *TelemetryEvent
}

// newQueue creates a bounded queue with the given capacity.
func newQueue(capacity int) *queue {
	return &queue{
		ch: make(chan *TelemetryEvent, capacity),
	}
}

// enqueue adds an event to the queue. If the queue is full the event is
// silently dropped and a warning is logged. This method never blocks.
func (q *queue) enqueue(event *TelemetryEvent) {
	select {
	case q.ch <- event:
	default:
		log.Printf("[nodeloom] queue full (capacity %d), dropping event type=%s", cap(q.ch), event.Type)
	}
}

// drain reads all currently buffered events (up to max) without blocking.
// It returns the events that were available at the time of the call.
func (q *queue) drain(max int) []*TelemetryEvent {
	var events []*TelemetryEvent
	for i := 0; i < max; i++ {
		select {
		case e := <-q.ch:
			events = append(events, e)
		default:
			return events
		}
	}
	return events
}

// channel returns the underlying channel for use with select statements.
func (q *queue) channel() <-chan *TelemetryEvent {
	return q.ch
}

// len returns the number of events currently in the queue.
func (q *queue) len() int {
	return len(q.ch)
}
