package telemetry

import (
	"context"

	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/andrewhowdencom/ore/x/llmbytes"
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
		case ledger.RoleUser, ledger.RoleSystem, ledger.RoleTool:
			counter = t.sent
		case ledger.RoleAssistant:
			counter = t.received
		default:
			return
		}

		if counter == nil {
			return
		}

		for _, art := range tc.Turn.Artifacts {
			n := llmbytes.Of(art)
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

// countBytes was removed in favor of x/llmbytes.Of, the single
// canonical implementation shared with x/analytics. See the matching
// note in x/analytics/analytics.go for the rationale.