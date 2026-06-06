package cognitive

import (
	"context"
	"fmt"

	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/state"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Pattern is a cognitive pattern that can run a multi-turn inference loop
// starting from a given state. Implementations decide when to stop based on
// the conversation state (e.g., when no pending tool calls remain).
type Pattern interface {
	Run(ctx context.Context, st state.State) (state.State, error)
}

// ReAct is a cognitive pattern that implements the ReAct feedback loop:
// it repeatedly invokes Step.Turn() while the last turn in state is not
// from the assistant (indicating pending tool results), driving the
// assistant to reason, act, and observe until no more tool calls remain.
type ReAct struct {
	Step     *loop.Step
	Provider provider.Provider
	tracer   trace.Tracer
}

// Compile-time assertion that ReAct implements Pattern.
var _ Pattern = (*ReAct)(nil)

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
		result, err := r.Step.Turn(ctx, st, r.Provider)
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
func NewTurnProcessor(factory func(*loop.Step, provider.Provider, trace.Tracer) Pattern, tracer trace.Tracer) session.TurnProcessor {
	return func(ctx context.Context, step *loop.Step, st state.State, prov provider.Provider) (state.State, error) {
		pattern := factory(step, prov, tracer)
		return pattern.Run(ctx, st)
	}
}

// ReActFactory returns a Pattern that runs the ReAct cognitive loop.
func ReActFactory(step *loop.Step, prov provider.Provider, tracer trace.Tracer) Pattern {
	return &ReAct{Step: step, Provider: prov, tracer: tracer}
}
