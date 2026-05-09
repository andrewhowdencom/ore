package loop

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/andrewhowdencom/tack/artifact"
	"github.com/andrewhowdencom/tack/provider"
	"github.com/andrewhowdencom/tack/state"
	"github.com/andrewhowdencom/tack/surface"
)

// BeforeTurn transforms state before the provider call.
type BeforeTurn interface {
	BeforeTurn(ctx context.Context, s state.State) (state.State, error)
}

// Step executes a single complete inference turn: it invokes the provider,
// optionally routes streaming deltas to a surface in real-time, and runs
// registered artifact handlers synchronously on the complete response.
type Step struct {
	surface     surface.Surface
	beforeTurns []BeforeTurn
	handlers    []Handler
}

// New creates a Step with the given options.
func New(opts ...Option) *Step {
	s := &Step{}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Option configures a Step.
type Option func(*Step)

// WithSurface configures a surface for streaming delta rendering.
func WithSurface(surf surface.Surface) Option {
	return func(s *Step) {
		s.surface = surf
	}
}

// WithBeforeTurn configures before-turn hooks that run before the provider
// call. Hooks run in registration order; each receives the state returned by
// the previous hook. An error from any hook aborts the turn.
func WithBeforeTurn(beforeTurns ...BeforeTurn) Option {
	return func(s *Step) {
		s.beforeTurns = beforeTurns
	}
}

// WithHandlers configures artifact handlers to run after each turn.
func WithHandlers(handlers ...Handler) Option {
	return func(s *Step) {
		s.handlers = handlers
	}
}

// Turn performs one inference turn with the given provider.
// If a surface is configured and the provider supports streaming, deltas
// are routed to the surface in real-time via a background goroutine.
// After the turn completes, all registered handlers are invoked on each
// artifact from the assistant turn. The operation is fully synchronous and
// blocking.
func (s *Step) Turn(ctx context.Context, st state.State, p provider.Provider) (state.State, error) {
	var err error

	for _, bt := range s.beforeTurns {
		st, err = bt.BeforeTurn(ctx, st)
		if err != nil {
			return st, fmt.Errorf("before turn hook failed: %w", err)
		}
	}

	var deltasCh chan artifact.Artifact
	if s.surface != nil {
		deltasCh = make(chan artifact.Artifact, 100)
	}

	var wg sync.WaitGroup
	if s.surface != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for delta := range deltasCh {
				if err := s.surface.RenderDelta(ctx, delta); err != nil {
					slog.Error("render delta failed", "err", err, "kind", delta.Kind())
				}
			}
		}()
	}

	var artifacts []artifact.Artifact

	if deltasCh != nil {
		if sp, ok := p.(provider.StreamingProvider); ok {
			artifacts, err = sp.InvokeStreaming(ctx, st, deltasCh)
		} else {
			artifacts, err = p.Invoke(ctx, st)
		}
	} else {
		artifacts, err = p.Invoke(ctx, st)
	}

	if deltasCh != nil {
		close(deltasCh)
		wg.Wait()
	}

	if err != nil {
		return st, fmt.Errorf("turn failed: %w", err)
	}

	st.Append(state.RoleAssistant, artifacts...)

	turns := st.Turns()
	if len(turns) == 0 {
		return st, nil
	}

	last := turns[len(turns)-1]
	if last.Role != state.RoleAssistant {
		return st, nil
	}

	for _, art := range last.Artifacts {
		for _, h := range s.handlers {
			if err := h.Handle(ctx, art, st); err != nil {
				return st, fmt.Errorf("artifact handler failed: %w", err)
			}
		}
	}

	return st, nil
}
