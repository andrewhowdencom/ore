package loop

import (
	"context"

	"github.com/andrewhowdencom/ore/ledger"
)

// EventBus owns the broadcast infrastructure: event channel, FanOut,
// synchronous OnEmit callbacks, and the bound state for auto-append.
// It is the single gateway for all observable mutations emitted by
// loop components. Step delegates Emit, Subscribe, and Close to it.
type EventBus struct {
	events chan outputEventEnvelope
	fanOut *FanOut
	onEmit []OnEmit
	bound  ledger.State
}

// newEventBus creates an EventBus with a fresh channel and FanOut.
func newEventBus() *EventBus {
	events := make(chan outputEventEnvelope)
	return &EventBus{
		events: events,
		fanOut: NewFanOut(events),
	}
}

// Emit runs all registered OnEmit callbacks synchronously, then sends the
// event to the FanOut and blocks until it has been delivered.
// When a state has been bound via WithState, only TurnCompleteEvent is
// automatically appended to that state before OnEmit callbacks run. Other
// event types are passed through unchanged.
func (eb *EventBus) Emit(ctx context.Context, event OutputEvent) {
	if tc, ok := event.(TurnCompleteEvent); ok && eb.bound != nil {
		eb.bound.Append(tc.Turn.Role, tc.Turn.Artifacts...)
	}
	for _, fn := range eb.onEmit {
		fn(ctx, event)
	}
	env := outputEventEnvelope{event: event, done: make(chan struct{})}
	select {
	case eb.events <- env:
	case <-ctx.Done():
		return
	}
	select {
	case <-env.done:
	case <-ctx.Done():
	}
}

// Subscribe returns a receive-only channel of OutputEvents whose Kind()
// matches any of the given kinds. The channel is closed when the EventBus's
// FanOut is closed. Events are delivered non-blocking; slow subscribers
// may drop events.
func (eb *EventBus) Subscribe(kinds ...string) <-chan OutputEvent {
	return eb.fanOut.Subscribe(kinds...)
}

// Close stops the EventBus's FanOut and closes all subscriber channels.
func (eb *EventBus) Close() error {
	return eb.fanOut.Close()
}
