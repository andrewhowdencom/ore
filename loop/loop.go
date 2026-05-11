package loop

import (
	"context"
	"fmt"
	"sync"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/state"
)

// BeforeTurn transforms state before the provider call.
type BeforeTurn interface {
	BeforeTurn(ctx context.Context, s state.State) (state.State, error)
}

// OutputEvent is emitted by Step during Turn() to notify subscribers of
// streaming progress and completed turns.
type OutputEvent interface {
	Kind() string
}

// DeltaEvent is emitted for each streaming delta artifact.
type DeltaEvent struct {
	Delta artifact.Artifact
}

// Kind returns the event kind identifier.
func (e DeltaEvent) Kind() string { return "delta" }

// TurnCompleteEvent is emitted when an assistant turn has been fully
// appended to state and all handlers have run.
type TurnCompleteEvent struct {
	Turn state.Turn
}

// Kind returns the event kind identifier.
func (e TurnCompleteEvent) Kind() string { return "turn_complete" }

// Step executes a single complete inference turn: it invokes the provider,
// optionally emits streaming deltas as OutputEvents, and runs
// registered artifact handlers synchronously on the complete response.
type Step struct {
	output      chan<- OutputEvent
	beforeTurns []BeforeTurn
	handlers    []Handler
	invokeOpts  []provider.InvokeOption
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

// WithOutput configures an output channel for streaming delta and
// turn completion events. The channel is written to during Turn();
// the caller must read from it or provide sufficient buffer capacity.
func WithOutput(ch chan<- OutputEvent) Option {
	return func(s *Step) {
		s.output = ch
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

// WithInvokeOptions configures pre-bound provider invocation options that are
// automatically passed to every provider call made by this Step.
func WithInvokeOptions(opts ...provider.InvokeOption) Option {
	return func(s *Step) {
		s.invokeOpts = opts
	}
}

// Turn performs one inference turn with the given provider.
// If an output channel is configured and the provider supports streaming, deltas
// are emitted as DeltaEvent to the channel in real-time via a background goroutine.
// After the turn completes, all registered handlers are invoked on each
// artifact from the assistant turn. The operation is fully synchronous and
// blocking.
func (s *Step) Turn(ctx context.Context, st state.State, p provider.Provider, opts ...provider.InvokeOption) (state.State, error) {
	var err error

	for _, bt := range s.beforeTurns {
		st, err = bt.BeforeTurn(ctx, st)
		if err != nil {
			return st, fmt.Errorf("before turn hook failed: %w", err)
		}
	}

	var deltasCh chan artifact.Artifact
	if s.output != nil {
		deltasCh = make(chan artifact.Artifact, 100)
	}

	var wg sync.WaitGroup
	if deltasCh != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for delta := range deltasCh {
				select {
				case s.output <- DeltaEvent{Delta: delta}:
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	allOpts := make([]provider.InvokeOption, 0, len(s.invokeOpts)+len(opts))
	allOpts = append(allOpts, s.invokeOpts...)
	allOpts = append(allOpts, opts...)

	var artifacts []artifact.Artifact

	if deltasCh != nil {
		if sp, ok := p.(provider.StreamingProvider); ok {
			artifacts, err = sp.InvokeStreaming(ctx, st, deltasCh, allOpts...)
		} else {
			artifacts, err = p.Invoke(ctx, st, allOpts...)
		}
	} else {
		artifacts, err = p.Invoke(ctx, st, allOpts...)
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

	if s.output != nil {
		select {
		case s.output <- TurnCompleteEvent{Turn: last}:
		case <-ctx.Done():
		}
	}

	return st, nil
}
