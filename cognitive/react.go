package cognitive

import (
	"context"
	"fmt"

	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/state"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Pattern is a cognitive pattern that can run a multi-turn inference loop
// starting from a given state. Implementations decide when to stop based on
// the conversation state (e.g., when no pending tool calls remain).
//
// Name returns a stable, lowercase identifier for the pattern (e.g.
// "react", "single_shot", "verified") used by the agent bundle for tracing
// and observability. It is part of the Pattern contract: any new pattern
// must implement it.
type Pattern interface {
	Run(ctx context.Context, st state.State) (state.State, error)
	Name() string
}

// ReAct is a cognitive pattern that implements the ReAct feedback loop:
// it repeatedly invokes Step.Turn() while the last turn in state is not
// from the assistant (indicating pending tool results), driving the
// assistant to reason, act, and observe until no more tool calls remain.
type ReAct struct {
	Step     loop.TurnRunner
	Provider provider.Provider
	Spec     models.Spec
	tracer   trace.Tracer
}

// Compile-time assertion that ReAct implements Pattern.
var _ Pattern = (*ReAct)(nil)

// SetRuntime is implemented by patterns that want the agent bundle
// to inject its runtime dependencies at construction. The agent's
// New type-asserts to this anonymous interface and calls it after
// building the step. Patterns that do not implement SetRuntime
// cannot be used with the agent bundle (their Step/Provider/Spec
// fields would remain nil).
func (r *ReAct) SetRuntime(step loop.TurnRunner, provider provider.Provider, spec models.Spec, tracer trace.Tracer) {
	r.Step = step
	r.Provider = provider
	r.Spec = spec
	r.tracer = tracer
}

// Name returns the pattern identifier, used by the agent bundle for
// tracing the agent.run span. Stable across versions.
func (r *ReAct) Name() string { return "react" }

// Run executes the ReAct feedback loop starting from the given state.
// It returns when the last turn is from the assistant (no pending tool
// calls) or when the context is cancelled.
func (r *ReAct) Run(ctx context.Context, st state.State) (state.State, error) {
	if r.tracer != nil {
		var span trace.Span
		ctx, span = r.tracer.Start(ctx, "react.run", trace.WithSpanKind(trace.SpanKindInternal))
		if id, ok := loop.ThreadIDFrom(ctx); ok {
			span.SetAttributes(attribute.String("thread_id", id))
		}
		defer span.End()
	}

	for {
		result, err := r.Step.Turn(ctx, st, r.Spec, r.Provider)
		if err != nil {
			return result, fmt.Errorf("react turn failed: %w", err)
		}

		turns := result.Turns()
		if len(turns) == 0 {
			return result, nil
		}

		last := turns[len(turns)-1]
		if last.Role == state.RoleAssistant {
			return result, nil
		}

		st = result
	}
}

// NewTurnProcessor returns a session.TurnProcessor that runs the given
// Pattern factory for each turn. The factory receives the session's
// loop.Step and provider so it can construct stateful Patterns like ReAct.
func NewTurnProcessor(factory func(loop.TurnExecutor, provider.Provider, trace.Tracer) Pattern, tracer trace.Tracer) session.TurnProcessor {
	return func(ctx context.Context, step *loop.Step, st state.State, prov provider.Provider, spec models.Spec) (state.State, error) {
		pattern := factory(step, prov, tracer)
		_ = pattern
		_ = spec
		return pattern.Run(ctx, st)
	}
}

// ReActFactory returns a Pattern that runs the ReAct cognitive loop.
func ReActFactory(step loop.TurnExecutor, prov provider.Provider, tracer trace.Tracer) Pattern {
	return &ReAct{Step: step, Provider: prov, tracer: tracer}
}
