package cognitive

import (
	"context"

	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/state"
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
// resulting state. The pattern does no terminal-detection; the
// caller's agent configuration (transforms, handlers) determines
// what happens to the produced turn.
func (s *SingleShot) Run(ctx context.Context, st state.State) (state.State, error) {
	return s.Step.Turn(ctx, st, s.Spec, s.Provider)
}

// Name returns the pattern identifier, used by the agent bundle for
// tracing the agent.run span. Stable across versions.
func (s *SingleShot) Name() string { return "single_shot" }
