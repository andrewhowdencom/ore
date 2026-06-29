package cognitive

import (
	"context"

	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/ledger"
	"go.opentelemetry.io/otel/trace"
)

// SingleShot is a cognitive pattern that runs exactly one inference
// turn and returns. It is the strategy for callers that want "ask the
// model once and act on the response" — compaction, judging,
// auto-titling, code review, test generation.
//
// The pattern has no loop and no terminal-detection: it is a
// zero-state one-step over the underlying loop.TurnRunner. Callers
// compose the SingleShot agent with WithTransforms/WithHandlers in
// the agent bundle to apply the same configuration (compaction
// transform, system prompt, telemetry handler) as the main conversation.
type SingleShot struct {
	Step     loop.TurnRunner
	Provider provider.Provider
	Spec     models.Spec
}

// Compile-time assertion that SingleShot implements Pattern.
var _ Pattern = (*SingleShot)(nil)

// Run invokes the underlying step exactly once and returns the
// resulting ledger. The pattern does no terminal-detection; the
// caller's agent configuration (transforms, handlers) determines
// what happens to the produced turn.
func (s *SingleShot) Run(ctx context.Context, st ledger.State) (ledger.State, error) {
	return s.Step.Turn(ctx, st, s.Spec, s.Provider)
}

// Name returns the pattern identifier, used by the agent bundle for
// tracing the agent.run span. Stable across versions.
func (s *SingleShot) Name() string { return "single_shot" }

// SetRuntime is implemented by patterns that want the agent bundle
// to inject its runtime dependencies at construction. The agent's
// New type-asserts to this anonymous interface and calls it after
// building the step. Patterns that do not implement SetRuntime
// cannot be used with the agent bundle (their Step/Provider/Spec
// fields would remain nil).
//
// tracer is accepted for interface uniformity but is unused by
// SingleShot (the pattern emits no spans of its own; the agent's
// agent.run span is the only one).
func (s *SingleShot) SetRuntime(step loop.TurnRunner, provider provider.Provider, spec models.Spec, _ trace.Tracer) {
	s.Step = step
	s.Provider = provider
	s.Spec = spec
}
