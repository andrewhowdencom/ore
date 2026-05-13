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

// OutputEvent represents any event emitted by a Step. Artifacts
// (e.g. TextDelta, ReasoningDelta) and turn-related events
// (TurnCompleteEvent, ErrorEvent) all implement this interface via
// their Kind() method, allowing subscribers to filter the event
// stream by kind (e.g. "text_delta", "turn_complete", "error").
type OutputEvent interface {
	Kind() string
}

// TurnCompleteEvent is emitted when an assistant turn has been fully
// appended to state and all handlers have run.
type TurnCompleteEvent struct {
	Turn state.Turn
}

// Kind returns the event kind identifier.
func (e TurnCompleteEvent) Kind() string { return "turn_complete" }

// ErrorEvent is emitted when a turn fails due to a provider or handler error.
type ErrorEvent struct {
	Err error
}

// Kind returns the event kind identifier.
func (e ErrorEvent) Kind() string { return "error" }

// Step executes a single complete inference turn: it invokes the provider,
// distributes streaming artifacts to subscribers via an embedded FanOut, and
// runs registered artifact handlers synchronously on the complete response.
type Step struct {
	events      chan OutputEvent
	fanOut      *FanOut
	beforeTurns []BeforeTurn
	handlers    []Handler
	invokeOpts  []provider.InvokeOption
}

// New creates a Step with the given options.
func New(opts ...Option) *Step {
	events := make(chan OutputEvent, 100)
	s := &Step{
		events: events,
		fanOut: NewFanOut(events),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Subscribe returns a receive-only channel of OutputEvents whose Kind()
// matches any of the given kinds. The channel is closed when the Step's
// FanOut is closed. Events are delivered non-blocking; slow subscribers
// may drop events.
func (s *Step) Subscribe(kinds ...string) <-chan OutputEvent {
	return s.fanOut.Subscribe(kinds...)
}

// Close stops the Step's FanOut and closes all subscriber channels.
func (s *Step) Close() error {
	return s.fanOut.Close()
}

// Option configures a Step.
type Option func(*Step)

// WithBeforeTurn configures before-turn hooks that run before any turn,
// including both inference turns (Turn) and submitted turns (Submit).
// Hooks run in registration order; each receives the state returned by
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
// The provider emits artifacts to a channel; all artifacts are forwarded to
// the Step's FanOut subscribers immediately as they arrive. Artifacts are
// accumulated into ordered blocks within the current turn: same-kind adjacent
// deltas merge into one block, and a kind switch starts a new block. The
// accumulated turn is appended to state once the provider returns. After the
// turn completes, all registered handlers are invoked on each artifact from
// the assistant turn. The operation is fully synchronous and blocking.
func (s *Step) Turn(ctx context.Context, st state.State, p provider.Provider, opts ...provider.InvokeOption) (state.State, error) {
	var err error

	for _, bt := range s.beforeTurns {
		st, err = bt.BeforeTurn(ctx, st)
		if err != nil {
			return st, fmt.Errorf("before turn hook failed: %w", err)
		}
	}

	provCh := make(chan artifact.Artifact, 100)
	var accumulatedArtifacts []artifact.Artifact

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for art := range provCh {
			select {
			case s.events <- art:
			case <-ctx.Done():
				return
			}
			switch d := art.(type) {
			case artifact.TextDelta:
				if len(accumulatedArtifacts) > 0 {
					if last, ok := accumulatedArtifacts[len(accumulatedArtifacts)-1].(artifact.Text); ok {
						last.Content += d.Content
						accumulatedArtifacts[len(accumulatedArtifacts)-1] = last
						continue
					}
				}
				accumulatedArtifacts = append(accumulatedArtifacts, artifact.Text(d))
			case artifact.ReasoningDelta:
				if len(accumulatedArtifacts) > 0 {
					if last, ok := accumulatedArtifacts[len(accumulatedArtifacts)-1].(artifact.Reasoning); ok {
						last.Content += d.Content
						accumulatedArtifacts[len(accumulatedArtifacts)-1] = last
						continue
					}
				}
				accumulatedArtifacts = append(accumulatedArtifacts, artifact.Reasoning(d))
			default:
				accumulatedArtifacts = append(accumulatedArtifacts, art)
			}
		}
	}()

	allOpts := make([]provider.InvokeOption, 0, len(s.invokeOpts)+len(opts))
	allOpts = append(allOpts, s.invokeOpts...)
	allOpts = append(allOpts, opts...)

	err = p.Invoke(ctx, st, provCh, allOpts...)
	close(provCh)
	wg.Wait()

	if err != nil {
		select {
		case s.events <- ErrorEvent{Err: err}:
		case <-ctx.Done():
		}
		return st, fmt.Errorf("turn failed: %w", err)
	}

	return s.finalizeTurn(ctx, st, state.RoleAssistant, accumulatedArtifacts)
}

// Submit records a non-inference turn into state, runs registered handlers,
// and emits a TurnCompleteEvent to all subscribers. It is the canonical
// mechanism for user, system, or tool turns to enter the same artifact stream
// as assistant responses from Turn().
func (s *Step) Submit(ctx context.Context, st state.State, role state.Role, artifacts ...artifact.Artifact) (state.State, error) {
	var err error

	for _, bt := range s.beforeTurns {
		st, err = bt.BeforeTurn(ctx, st)
		if err != nil {
			return st, fmt.Errorf("before turn hook failed: %w", err)
		}
	}

	return s.finalizeTurn(ctx, st, role, artifacts)
}

// finalizeTurn appends a turn to state, runs registered handlers on each
// artifact, and emits a TurnCompleteEvent to all subscribers. It is the shared
// post-processing pipeline used by both Turn() and Submit().
func (s *Step) finalizeTurn(ctx context.Context, st state.State, role state.Role, artifacts []artifact.Artifact) (state.State, error) {
	st.Append(role, artifacts...)

	turns := st.Turns()
	if len(turns) == 0 {
		return st, nil
	}

	last := turns[len(turns)-1]
	if last.Role != role {
		return st, nil
	}

	for _, art := range last.Artifacts {
		for _, h := range s.handlers {
			if err := h.Handle(ctx, art, st); err != nil {
				return st, fmt.Errorf("artifact handler failed: %w", err)
			}
		}
	}

	select {
	case s.events <- TurnCompleteEvent{Turn: last}:
	case <-ctx.Done():
	}

	return st, nil
}