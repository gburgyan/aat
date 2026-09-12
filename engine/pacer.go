package engine

import (
	"context"
	"sync"
	"time"

	"github.com/gburgyan/aat/adapter"
)

// Pacer spaces the starts of requests at least an interval apart. Engines that
// share one Pacer are paced together, so the runs of a parallel batch add up to
// no more than one request per interval. A nil *Pacer does not pace.
type Pacer struct {
	interval time.Duration

	mu   sync.Mutex
	next time.Time // earliest start of the next request
}

// NewPacer returns a Pacer for interval, or nil when interval is not positive.
func NewPacer(interval time.Duration) *Pacer {
	if interval <= 0 {
		return nil
	}
	return &Pacer{interval: interval}
}

// Wait blocks until the caller's request may start and reserves that slot. It
// returns ctx's error if ctx is done first; the slot stays taken.
func (p *Pacer) Wait(ctx context.Context) error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	start := time.Now()
	if p.next.After(start) {
		start = p.next
	}
	p.next = start.Add(p.interval)
	p.mu.Unlock()

	delay := time.Until(start)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// WithPacer sends every request of the engine, including retries, verification,
// and cleanup, through p. Engines that share p are paced together; a nil p turns
// pacing off.
func (e *Engine) WithPacer(p *Pacer) *Engine {
	e.pacer = p
	return e
}

// send waits for the engine's pacer, then executes req.
func (e *Engine) send(ctx context.Context, exec *adapter.HTTPExecutor, req *adapter.Request) (*adapter.Response, error) {
	if err := e.pacer.Wait(ctx); err != nil {
		return nil, err
	}
	return exec.Execute(ctx, req)
}
