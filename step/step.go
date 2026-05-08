package step

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/andrewhowdencom/tack/artifact"
	"github.com/andrewhowdencom/tack/core"
	"github.com/andrewhowdencom/tack/provider"
	"github.com/andrewhowdencom/tack/state"
	"github.com/andrewhowdencom/tack/surface"
)

// Step executes a single complete inference turn: it calls core.Turn(),
// routes streaming deltas to the surface in real-time, and runs registered
// artifact handlers synchronously on the complete response.
type Step struct {
	core     *core.Loop
	surface  surface.Surface
	handlers []Handler
}

// New creates a Step with the given core loop, surface, and handlers.
func New(loop *core.Loop, surf surface.Surface, handlers ...Handler) *Step {
	return &Step{
		core:     loop,
		surface:  surf,
		handlers: handlers,
	}
}

// Execute performs one inference turn with the given provider.
// If the provider supports streaming, deltas are routed to the surface
// in real-time via a background goroutine. After the turn completes,
// all registered handlers are invoked on each artifact from the
// assistant turn. The operation is fully synchronous and blocking.
func (s *Step) Execute(ctx context.Context, st state.State, p provider.Provider) (state.State, error) {
	deltasCh := make(chan artifact.Artifact, 100)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for delta := range deltasCh {
			if err := s.surface.RenderDelta(ctx, delta); err != nil {
				slog.Error("render delta failed", "err", err, "kind", delta.Kind())
			}
		}
	}()

	result, err := s.core.Turn(ctx, st, p, deltasCh)

	close(deltasCh)
	wg.Wait()

	if err != nil {
		return result, fmt.Errorf("turn failed: %w", err)
	}

	turns := result.Turns()
	if len(turns) == 0 {
		return result, nil
	}

	last := turns[len(turns)-1]
	if last.Role != state.RoleAssistant {
		return result, nil
	}

	for _, art := range last.Artifacts {
		for _, h := range s.handlers {
			if err := h.Handle(ctx, art, result); err != nil {
				return result, fmt.Errorf("artifact handler failed: %w", err)
			}
		}
	}

	return result, nil
}
