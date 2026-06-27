package junk

import (
	"context"
	"encoding/json"

	"github.com/andrewhowdencom/ore/loop"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// Event is the base interface for all ingress events to a session Stream.
// Custom event types can be defined in other packages by implementing
// the public Kind() and Context() methods.
type Event interface {
	Kind() string
	Context() context.Context
}

// UserMessageEvent represents the user submitting a text message.
type UserMessageEvent struct {
	// Content holds the raw user message text.
	Content string

	// Ctx carries the provenance/context metadata for the user message.
	Ctx context.Context
}

// Kind returns the event kind identifier.
func (e UserMessageEvent) Kind() string { return "user_message" }

// Context returns the event's context.Context metadata.
func (e UserMessageEvent) Context() context.Context { return e.Ctx }

// InterruptEvent represents the user interrupting the current operation.
type InterruptEvent struct {
	// Ctx carries the provenance/context metadata for the interrupt event.
	Ctx context.Context
}

// Kind returns the event kind identifier.
func (e InterruptEvent) Kind() string { return "interrupt" }

// Context returns the event's context.Context metadata.
func (e InterruptEvent) Context() context.Context { return e.Ctx }

// InterceptResult is the result of an interceptor processing an event.
//
// Notice carries ephemeral, user-visible messages that are emitted as
// loop.NoticeEvent and never persisted to state. Each Notice carries
// a Severity that conduits use to pick a rendering style (Success, Info,
// Warn, Error). A nil slice means no notices were produced.
type InterceptResult struct {
	// Event is the replacement event to continue processing. If nil, the
	// original event is consumed and no further processing occurs.
	Event Event
	// Notice is the list of ephemeral, user-visible messages emitted as
	// loop.NoticeEvent after Intercept returns.
	Notice []loop.Notice
}

// Interceptor processes events before they enter the LLM pipeline.
// It receives a junk.Event, the active *junk.Stream, and a
// loop.Emitter for signaling activity, and can either:
//   - Return a non-nil Event in the result to rewrite the event
//   - Return a nil Event in the result to consume the event (no further processing)
//   - Return an error to abort processing
//
// Notice messages are ephemeral UI messages that are not persisted to
// state. Each Notice carries a Severity so conduits can pick a rendering
// style.
//
// The *junk.Stream parameter is the stream that owns the in-flight event,
// so interceptors can call stream-scoped methods like SetMetadata. It is
// never nil for a configured interceptor — stream.processOne passes the
// active stream unconditionally.
type Interceptor interface {
	Intercept(ctx context.Context, event Event, stream *Stream, emitter loop.Emitter) (InterceptResult, error)
}

// InterceptorFunc is a function type that implements Interceptor.
type InterceptorFunc func(ctx context.Context, event Event, stream *Stream, emitter loop.Emitter) (InterceptResult, error)

// Intercept delegates to the function.
func (f InterceptorFunc) Intercept(ctx context.Context, event Event, stream *Stream, emitter loop.Emitter) (InterceptResult, error) {
	return f(ctx, event, stream, emitter)
}

// SessionSwitchEvent signals a cross-session navigation.
type SessionSwitchEvent struct {
	SessionID string
	Ctx       context.Context
}

// Kind returns the event kind identifier.
func (e SessionSwitchEvent) Kind() string { return "session_switch" }

// Context returns the event's context.Context metadata.
func (e SessionSwitchEvent) Context() context.Context { return e.Ctx }

// MarshalJSON serializes the event to JSON. It includes the session_id
// and an optional context envelope with provenance and traceparent.
func (e SessionSwitchEvent) MarshalJSON() ([]byte, error) {
	type contextJSON struct {
		Provenance  string `json:"provenance,omitempty"`
		Traceparent string `json:"traceparent,omitempty"`
	}
	type output struct {
		Kind      string       `json:"kind"`
		SessionID string       `json:"session_id"`
		Context   *contextJSON `json:"context,omitempty"`
	}

	o := output{
		Kind:      "session_switch",
		SessionID: e.SessionID,
	}

	if prov, ok := loop.ProvenanceFrom(e.Ctx); ok && prov != "" {
		o.Context = &contextJSON{Provenance: prov}
	}

	if span := trace.SpanFromContext(e.Ctx); span.SpanContext().IsValid() {
		carrier := propagation.MapCarrier{}
		propagator := propagation.TraceContext{}
		propagator.Inject(e.Ctx, carrier)
		if tp := carrier.Get("traceparent"); tp != "" {
			if o.Context == nil {
				o.Context = &contextJSON{}
			}
			o.Context.Traceparent = tp
		}
	}

	return json.Marshal(o)
}
