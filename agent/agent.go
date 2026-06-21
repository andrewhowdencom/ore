package agent

import (
	"context"
	"sync"

	"github.com/andrewhowdencom/ore/cognitive"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/state"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Agent is a configured bundle of everything an inference call needs:
// provider, model spec, transforms, handlers, cognitive pattern, tracer,
// and (optionally) a state binding. The same Agent is reused across
// many Run calls; differences between "kinds" of agent (ReAct,
// SingleShot, Verified) live in the configured pattern, not in the
// agent type.
type Agent struct {
	name string

	provider   provider.Provider
	spec       models.Spec
	transforms []loop.Transform
	handlers   []loop.Handler
	invokeOpts []provider.InvokeOption
	pattern    cognitive.Pattern
	state      state.State
	tracer     trace.Tracer

	closeOnce sync.Once
	closeErr  error
	step      *loop.Step
}

// New constructs an Agent with the given name and options. The pattern
// must be configured via WithPattern; if not, New panics.
//
// New builds an internal *loop.Step from the configured options. The
// step is reused across Run calls.
func New(name string, opts ...Option) *Agent {
	a := &Agent{name: name}
	for _, opt := range opts {
		opt(a)
	}
	if a.pattern == nil {
		panic("agent.New: WithPattern is required")
	}

	stepOpts := []loop.Option{
		loop.WithTransforms(a.transforms...),
		loop.WithHandlers(a.handlers...),
		loop.WithInvokeOptions(a.invokeOpts...),
		loop.WithDefaultSpec(a.spec),
		loop.WithTracer(a.tracer),
	}
	if a.state != nil {
		stepOpts = append(stepOpts, loop.WithState(a.state))
	}
	a.step = loop.New(stepOpts...)

	// Inject the agent's runtime dependencies into the pattern. Patterns
	// implement SetRuntime to opt in; this lets the agent own the
	// step's lifecycle while the pattern keeps strongly-typed
	// references for the duration of Run.
	if setter, ok := a.pattern.(interface {
		SetRuntime(loop.TurnRunner, provider.Provider, models.Spec, trace.Tracer)
	}); ok {
		setter.SetRuntime(a.step, a.provider, a.spec, a.tracer)
	}
	return a
}

// Run executes the configured pattern against the given state. The
// caller may invoke Run any number of times; the same internal step
// is reused.
//
// When WithState was used at construction, Run auto-appends the
// produced turn to the bound state (via the underlying step's
// Emit/TurnCompleteEvent path).
//
// When a tracer is configured, Run records an "agent.run" span
// (Internal) with agent.name and agent.pattern attributes. The span
// is the parent of any loop.turn span emitted by Step.Turn.
func (a *Agent) Run(ctx context.Context, st state.State) (state.State, error) {
	if a.tracer != nil {
		var span trace.Span
		ctx, span = a.tracer.Start(ctx, "agent.run",
			trace.WithSpanKind(trace.SpanKindInternal),
			trace.WithAttributes(
				attribute.String("agent.name", a.name),
				attribute.String("agent.pattern", a.pattern.Name()),
			),
		)
		defer span.End()
	}
	return a.pattern.Run(ctx, st)
}

// Subscribe returns a filtered output event channel from the agent's
// internal step. It is a thin pass-through to step.Subscribe; the
// event surface is owned by the loop.
//
// The returned channel is closed when the agent is Closed (or when the
// step's underlying EventBus closes for any other reason).
func (a *Agent) Subscribe(kinds ...string) <-chan loop.OutputEvent {
	return a.step.Subscribe(kinds...)
}

// Close stops the agent's internal step and closes all subscriber
// channels. Safe to call multiple times.
func (a *Agent) Close() error {
	a.closeOnce.Do(func() {
		a.closeErr = a.step.Close()
	})
	return a.closeErr
}

// Name returns the agent's identifier (the value passed to New).
func (a *Agent) Name() string { return a.name }

// Step returns the agent's internal loop.Step. Exposed for advanced
// wiring (sub-agents, benchmarks, custom handlers that need direct
// access to the underlying emitter). Most callers should use
// Run/Subscribe/Close.
func (a *Agent) Step() *loop.Step { return a.step }

// Provider returns the agent's configured provider.
func (a *Agent) Provider() provider.Provider { return a.provider }

// Spec returns the agent's configured model spec.
func (a *Agent) Spec() models.Spec { return a.spec }

// Pattern returns the agent's configured cognitive pattern.
func (a *Agent) Pattern() cognitive.Pattern { return a.pattern }
