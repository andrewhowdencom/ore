package telemetry

import (
	"context"
	"encoding/json"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/state"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Telemetry records OpenTelemetry metrics for ore conversation turns.
type Telemetry struct {
	sent     metric.Int64Counter
	received metric.Int64Counter
}

// New creates a Telemetry with the given meter. If meter is nil, all
// recording operations are no-ops.
func New(meter metric.Meter) *Telemetry {
	if meter == nil {
		return &Telemetry{}
	}

	sent, err := meter.Int64Counter("llm.bytes.sent")
	if err != nil {
		return &Telemetry{}
	}

	received, err := meter.Int64Counter("llm.bytes.received")
	if err != nil {
		return &Telemetry{}
	}

	return &Telemetry{
		sent:     sent,
		received: received,
	}
}

// OnEmit returns a loop.OnEmit callback that records metrics for
// TurnCompleteEvent. All other event types are ignored.
func (t *Telemetry) OnEmit() loop.OnEmit {
	if t.sent == nil && t.received == nil {
		return func(context.Context, loop.OutputEvent) {}
	}

	return func(ctx context.Context, event loop.OutputEvent) {
		tc, ok := event.(loop.TurnCompleteEvent)
		if !ok {
			return
		}

		var counter metric.Int64Counter
		switch tc.Turn.Role {
		case state.RoleUser, state.RoleSystem, state.RoleTool:
			counter = t.sent
		case state.RoleAssistant:
			counter = t.received
		default:
			return
		}

		if counter == nil {
			return
		}

		for _, art := range tc.Turn.Artifacts {
			n := countBytes(art)
			if n == 0 {
				continue
			}

			counter.Add(ctx, n, metric.WithAttributes(
				attribute.String("artifact.kind", art.Kind()),
				attribute.String("role", string(tc.Turn.Role)),
			))
		}
	}
}

func countBytes(art artifact.Artifact) int64 {
	switch a := art.(type) {
	case artifact.Text:
		return int64(len(a.Content))
	case artifact.Reasoning:
		return int64(len(a.Content))
	case artifact.ToolCall:
		return int64(len(a.LLMString()))
	case artifact.ToolResult:
		return int64(len(a.LLMString()))
	case artifact.Image:
		return int64(len(a.URL))
	case artifact.Usage:
		return 0
	default:
		if b, err := json.Marshal(art); err == nil {
			return int64(len(b))
		}
		return 0
	}
}