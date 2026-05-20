package loop

import (
	"context"
	"fmt"
	"sync"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/state"
)

// Transform modifies the state view presented to the provider during
// inference. Implementations must not mutate the underlying persistent
// buffer; they may return a derived state.State wrapper instead.
//
// Multiple transforms compose in registration order. Each transform
// receives the state returned by the previous one. An error from any
// transform aborts the turn before the provider is invoked.
type Transform interface {
	Transform(ctx context.Context, st state.State) (state.State, error)
}

// EventContext carries metadata for an event, analogous to context.Context.
// It travels with an event through the event stream so subscribers can
// access routing metadata (provenance, trace IDs, etc.) uniformly.
type EventContext struct {
	Provenance string
}

// OutputEvent represents any event emitted by a Step.
// All output events carry an EventContext so subscribers can access
// routing metadata uniformly. Events include wrapped artifacts
// (ArtifactEvent), turn completions (TurnCompleteEvent), and errors
// (ErrorEvent).
type OutputEvent interface {
	Kind() string
	Context() EventContext
}

// TurnCompleteEvent is emitted when an assistant turn has been fully
// appended to state and all handlers have run.
type TurnCompleteEvent struct {
	Turn state.Turn

	// Ctx carries routing metadata for the event, such as provenance
	// information for echo suppression.
	Ctx EventContext
}

// Kind returns the event kind identifier.
func (e TurnCompleteEvent) Kind() string { return "turn_complete" }

// Context returns the event context.
func (e TurnCompleteEvent) Context() EventContext { return e.Ctx }

// ErrorEvent is emitted when a turn fails due to a provider or handler error.
type ErrorEvent struct {
	Err error

	// Ctx carries routing metadata for the event, such as provenance
	// information for echo suppression.
	Ctx EventContext
}

// Kind returns the event kind identifier.
func (e ErrorEvent) Kind() string { return "error" }

// Context returns the event context.
func (e ErrorEvent) Context() EventContext { return e.Ctx }

// ArtifactEvent wraps an artifact.Artifact with an EventContext so it
// can be emitted as an OutputEvent without polluting the artifact type
// with routing metadata.
type ArtifactEvent struct {
	Artifact artifact.Artifact

	// Ctx carries routing metadata for the event, such as provenance
	// information for echo suppression.
	Ctx EventContext
}

// Kind returns the underlying artifact's kind.
func (e ArtifactEvent) Kind() string { return e.Artifact.Kind() }

// Context returns the event context.
func (e ArtifactEvent) Context() EventContext { return e.Ctx }

// outputEventEnvelope wraps an OutputEvent with an acknowledgment channel.
// The producer blocks until the FanOut closes done after delivering the event.
type outputEventEnvelope struct {
	event OutputEvent
	done  chan struct{}
}

// Step executes a single complete inference turn: it invokes the provider,
// distributes streaming artifacts to subscribers via an embedded FanOut, and
// runs registered artifact handlers synchronously on the complete response.
type Step struct {
	events        chan outputEventEnvelope
	fanOut        *FanOut
	transforms    []Transform
	handlers      []Handler
	invokeOpts    []provider.InvokeOption
	eventContext  EventContext
}

// New creates a Step with the given options.
func New(opts ...Option) *Step {
	events := make(chan outputEventEnvelope)
	s := &Step{
		events: events,
		fanOut: NewFanOut(events),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Emit sends an event to the FanOut and blocks until it has been delivered.
func (s *Step) Emit(ctx context.Context, event OutputEvent) {
	env := outputEventEnvelope{event: event, done: make(chan struct{})}
	select {
	case s.events <- env:
	case <-ctx.Done():
		return
	}
	select {
	case <-env.done:
	case <-ctx.Done():
	}
}

// Subscribe returns a receive-only channel of OutputEvents whose Kind()
// matches any of the given kinds. The channel is closed when the Step's
// FanOut is closed. Events are delivered non-blocking; slow subscribers
// may drop events.
func (s *Step) Subscribe(kinds ...string) <-chan OutputEvent {
	return s.fanOut.Subscribe(kinds...)
}

// SetEventContext sets the EventContext that will be attached to all
// subsequent output events emitted by this Step. It is used by
// Stream.Process to thread context from the input event through the
// turn pipeline. Callers must ensure this is called before Turn or
// Submit and cleared after (typically via defer).
func (s *Step) SetEventContext(ctx EventContext) {
	s.eventContext = ctx
}

// clearEventContext resets the EventContext on the Step to its zero
// value. It is the counterpart to SetEventContext and is invoked via
// defer in Turn and Submit to prevent context leakage between calls.
func (s *Step) clearEventContext() {
	s.eventContext = EventContext{}
}

// Close stops the Step's FanOut and closes all subscriber channels.
func (s *Step) Close() error {
	return s.fanOut.Close()
}

// Option configures a Step.
type Option func(*Step)

// WithTransforms configures inference assembly transforms that run
// before each provider call in Turn(). Transforms receive the state
// after any user/system/tool submissions and before the provider
// serializes it. They must not mutate the underlying buffer.
func WithTransforms(transforms ...Transform) Option {
	return func(s *Step) {
		s.transforms = transforms
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
	defer s.clearEventContext()
	var err error

	provCh := make(chan artifact.Artifact, 100)
	var accumulatedArtifacts []artifact.Artifact

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		var currentBlock artifact.Artifact

		for art := range provCh {
			switch d := art.(type) {
			case artifact.TextDelta:
				if text, ok := currentBlock.(artifact.Text); ok {
					text.Content += d.Content
					currentBlock = text
					s.Emit(ctx, ArtifactEvent{Artifact: art, Ctx: s.eventContext})
					if ctx.Err() != nil {
						return
					}
				} else {
					if currentBlock != nil {
						s.Emit(ctx, ArtifactEvent{Artifact: currentBlock, Ctx: s.eventContext})
						if ctx.Err() != nil {
							return
						}
						accumulatedArtifacts = append(accumulatedArtifacts, currentBlock)
					}
					currentBlock = artifact.Text(d)
					s.Emit(ctx, ArtifactEvent{Artifact: art, Ctx: s.eventContext})
					if ctx.Err() != nil {
						return
					}
				}
			case artifact.ReasoningDelta:
				if reasoning, ok := currentBlock.(artifact.Reasoning); ok {
					reasoning.Content += d.Content
					currentBlock = reasoning
					s.Emit(ctx, ArtifactEvent{Artifact: art, Ctx: s.eventContext})
					if ctx.Err() != nil {
						return
					}
				} else {
					if currentBlock != nil {
						s.Emit(ctx, ArtifactEvent{Artifact: currentBlock, Ctx: s.eventContext})
						if ctx.Err() != nil {
							return
						}
						accumulatedArtifacts = append(accumulatedArtifacts, currentBlock)
					}
					currentBlock = artifact.Reasoning(d)
					s.Emit(ctx, ArtifactEvent{Artifact: art, Ctx: s.eventContext})
					if ctx.Err() != nil {
						return
					}
				}
			default:
				if currentBlock != nil {
					s.Emit(ctx, ArtifactEvent{Artifact: currentBlock, Ctx: s.eventContext})
					if ctx.Err() != nil {
						return
					}
					accumulatedArtifacts = append(accumulatedArtifacts, currentBlock)
					currentBlock = nil
				}
				s.Emit(ctx, ArtifactEvent{Artifact: art, Ctx: s.eventContext})
				if ctx.Err() != nil {
					return
				}
				if _, ok := art.(artifact.ToolCallDelta); !ok {
					accumulatedArtifacts = append(accumulatedArtifacts, art)
				}
			}
		}
		if currentBlock != nil {
			s.Emit(ctx, ArtifactEvent{Artifact: currentBlock, Ctx: s.eventContext})
			if ctx.Err() != nil {
				return
			}
			accumulatedArtifacts = append(accumulatedArtifacts, currentBlock)
		}
	}()

	allOpts := make([]provider.InvokeOption, 0, len(s.invokeOpts)+len(opts))
	allOpts = append(allOpts, s.invokeOpts...)
	allOpts = append(allOpts, opts...)

	err = p.Invoke(ctx, st, provCh, allOpts...)
	close(provCh)
	wg.Wait()

	if err != nil {
		s.Emit(ctx, ErrorEvent{Err: err, Ctx: s.eventContext})
		
		return st, fmt.Errorf("turn failed: %w", err)
	}

	return s.finalizeTurn(ctx, st, state.RoleAssistant, accumulatedArtifacts)
}

// Submit records a non-inference turn into state, runs registered handlers,
// and emits a TurnCompleteEvent to all subscribers. It is the canonical
// mechanism for user, system, or tool turns to enter the same artifact stream
// as assistant responses from Turn().
func (s *Step) Submit(ctx context.Context, st state.State, role state.Role, artifacts ...artifact.Artifact) (state.State, error) {
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

	s.Emit(ctx, TurnCompleteEvent{Turn: last, Ctx: s.eventContext})
	

	return st, nil
}