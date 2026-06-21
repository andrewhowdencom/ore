package harness

import (
	"context"
	"errors"
	"sync"

	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/x/conduit"
)

// Host orchestrates multiple conduits with a shared session manager.
type Host struct {
	mgr      *session.Manager
	conduits []conduit.Conduit
}

// New creates a Host with the given session manager.
func New(mgr *session.Manager) *Host {
	return &Host{mgr: mgr}
}

// Add registers a conduit to be started by Run. Nil conduits are ignored.
func (h *Host) Add(c conduit.Conduit) {
	if c == nil {
		return
	}
	h.conduits = append(h.conduits, c)
}

// Run starts all registered conduits concurrently and blocks until ctx is
// cancelled or any conduit returns a non-nil error. When an error occurs,
// the context is cancelled to signal remaining conduits to shut down.
func (h *Host) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if len(h.conduits) == 0 {
		return nil
	}

	var wg sync.WaitGroup
	errCh := make(chan error, len(h.conduits))

	for _, c := range h.conduits {
		wg.Add(1)
		go func(c conduit.Conduit) {
			defer wg.Done()
			if err := c.Start(ctx); err != nil {
				if errors.Is(err, ctx.Err()) {
					return
				}
				select {
				case errCh <- err:
					cancel()
				case <-ctx.Done():
				}
			}
		}(c)
	}

	go func() {
		wg.Wait()
		close(errCh)
	}()

	for err := range errCh {
		if err != nil {
			return err
		}
	}
	return nil
}