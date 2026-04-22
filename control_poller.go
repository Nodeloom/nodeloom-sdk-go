package nodeloom

import (
	"log"
	"sync"
	"time"
)

// controlPoller refreshes the registry on a fixed interval. Telemetry batch
// responses already piggy-back the control payload, so this is mainly useful
// for sparse-traffic agents that may go minutes between traces.
type controlPoller struct {
	registry *ControlRegistry
	apiFunc  func() *ApiClient
	interval time.Duration
	stopOnce sync.Once
	stopChan chan struct{}
	doneChan chan struct{}
}

func newControlPoller(registry *ControlRegistry, apiFunc func() *ApiClient, interval time.Duration) *controlPoller {
	return &controlPoller{
		registry: registry,
		apiFunc:  apiFunc,
		interval: interval,
		stopChan: make(chan struct{}),
		doneChan: make(chan struct{}),
	}
}

func (p *controlPoller) start() {
	go p.run()
}

// stop signals the loop to exit and waits, bounded by timeout so shutdown
// cannot block indefinitely on a slow in-flight GetAgentControl request.
// If the tick is stuck in a network call it will finish on its own; the
// stopOnce guarantee means the second shutdown call is a no-op.
func (p *controlPoller) stop(timeout time.Duration) {
	p.stopOnce.Do(func() {
		close(p.stopChan)
		select {
		case <-p.doneChan:
		case <-time.After(timeout):
			log.Printf("[nodeloom] control poller stop timed out after %v (in-flight tick will finish in background)", timeout)
		}
	})
}

func (p *controlPoller) run() {
	defer close(p.doneChan)
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-p.stopChan:
			return
		case <-ticker.C:
			p.tick()
		}
	}
}

func (p *controlPoller) tick() {
	// Guard against panics in the ApiClient factory or inside GetAgentControl —
	// the poller goroutine must keep running across transient failures.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[nodeloom] control poller panic recovered: %v", r)
		}
	}()

	api := p.apiFunc()
	if api == nil {
		return
	}
	for _, name := range p.registry.KnownAgents() {
		// Bail fast when shutdown lands between per-agent calls so we
		// don't waste cycles iterating a long KnownAgents list.
		select {
		case <-p.stopChan:
			return
		default:
		}
		if _, err := api.GetAgentControl(name); err != nil {
			log.Printf("[nodeloom] control poll for %q failed: %v", name, err)
		}
	}
}
