package agent

import (
	"github.com/andrewhowdencom/ore/cognitive"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/ledger"
	"go.opentelemetry.io/otel/trace"
)

// Option configures an Agent. Options are applied in order at New; later
// options can append to slice-valued options (WithTransforms,
// WithHandlers, WithInvokeOptions).
type Option func(*Agent)

// WithProvider sets the provider used for inference. The provider is
// invoked by the agent's internal step.
func WithProvider(p provider.Provider) Option {
	return func(a *Agent) { a.provider = p }
}

// WithSpec sets the agent's model spec. It becomes the loop's default
// spec; per-call specs (set by the pattern) override it when non-empty.
func WithSpec(spec models.Spec) Option {
	return func(a *Agent) { a.spec = spec }
}

// WithTransforms registers loop.Transform values that run before each
// provider call. They apply to every inference the agent performs.
func WithTransforms(t ...loop.Transform) Option {
	return func(a *Agent) { a.transforms = append(a.transforms, t...) }
}

// WithHandlers registers loop.Handler values that run on the complete
// response after each turn. They apply to every inference the agent
// performs.
func WithHandlers(h ...loop.Handler) Option {
	return func(a *Agent) { a.handlers = append(a.handlers, h...) }
}

// WithPattern sets the cognitive pattern that drives Run. Required:
// New panics if WithPattern is not provided.
func WithPattern(p cognitive.Pattern) Option {
	return func(a *Agent) { a.pattern = p }
}

// WithTracer configures an OpenTelemetry tracer. When set, Run records
// an "agent.run" span (Internal) with agent.name and agent.pattern
// attributes, parent of any loop.turn span emitted by the underlying
// step.
func WithTracer(tracer trace.Tracer) Option {
	return func(a *Agent) { a.tracer = tracer }
}

// WithState binds a mutable state to the agent so that TurnCompleteEvents
// from Run auto-append to the ledger. If not set, Run does not auto-append;
// callers manage state themselves. Use LoadTurns to reset the bound
// state between Run calls.
func WithState(st ledger.State) Option {
	return func(a *Agent) { a.state = st }
}

// WithInvokeOptions configures per-call provider options forwarded to
// every provider call made by the agent's underlying step.
func WithInvokeOptions(opts ...provider.InvokeOption) Option {
	return func(a *Agent) { a.invokeOpts = append(a.invokeOpts, opts...) }
}
