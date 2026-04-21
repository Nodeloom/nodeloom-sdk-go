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
	registry  *ControlRegistry
	apiFunc   func() *ApiClient
	interval  time.Duration
	stopOnce  sync.Once
	stopChan  chan struct{}
	doneChan  chan struct{}
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

func (p *controlPoller) stop() {
	p.stopOnce.Do(func() {
		close(p.stopChan)
		<-p.doneChan
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
	api := p.apiFunc()
	if api == nil {
		return
	}
	for _, name := range p.registry.KnownAgents() {
		if _, err := api.GetAgentControl(name); err != nil {
			log.Printf("[nodeloom] control poll for %q failed: %v", name, err)
		}
	}
}
