package agent

import (
	"context"
	"errors"
	"sync"

	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/x/conduit"
)

// Agent orchestrates multiple conduits with a shared session manager.
type Agent struct {
	mgr      *session.Manager
	conduits []conduit.Conduit
}

// New creates an Agent with the given session manager.
func New(mgr *session.Manager) *Agent {
	return &Agent{mgr: mgr}
}

// Add registers a conduit to be started by Run. Nil conduits are ignored.
func (a *Agent) Add(c conduit.Conduit) {
	if c == nil {
		return
	}
	a.conduits = append(a.conduits, c)
}

// Run starts all registered conduits concurrently and blocks until ctx is
// cancelled or any conduit returns a non-nil error. When an error occurs,
// the context is cancelled to signal remaining conduits to shut down.
func (a *Agent) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if len(a.conduits) == 0 {
		return nil
	}

	var wg sync.WaitGroup
	errCh := make(chan error, len(a.conduits))

	for _, c := range a.conduits {
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
