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

// ReAct is a cognitive pattern that implements the ReAct feedback loop:
// it repeatedly invokes Step.Turn() while the last turn in state is not
// from the assistant (indicating pending tool results), driving the
// assistant to reason, act, and observe until no more tool calls remain.
type ReAct struct {
	Step     *loop.Step
	Provider provider.Provider
	tracer   trace.Tracer
}

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

// NewTurnProcessor returns a session.TurnProcessor that runs the ReAct
// cognitive pattern. It creates a temporary ReAct with the session's
// loop.Step and the Manager's provider for each turn. Pass a tracer to
// enable OpenTelemetry spans for the ReAct loop.
func NewTurnProcessor(tracer trace.Tracer) session.TurnProcessor {
	return func(ctx context.Context, step *loop.Step, st state.State, prov provider.Provider) (state.State, error) {
		react := &ReAct{Step: step, Provider: prov, tracer: tracer}
		return react.Run(ctx, st)
	}
}
