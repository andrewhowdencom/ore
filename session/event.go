// Package session provides the per-conversation primitives that replace
// the legacy junk.Thread / junk.Stream / junk.Manager combination. A
// Session is a per-conversation handle that owns its identity, ledger
// thread, conduit-mapping metadata, and the long-lived loop.Step used
// for subscriber fanout. A Runner is the process-wide wire that drives
// inference against sessions.
//
// The primitives intentionally split "data" from "control": Session is
// pure data; Runner is the active component that handles events, runs
// interceptors (e.g. slash commands), builds ephemeral agents, and
// routes output events through sink routers. This separation lets
// callers compose the parts they need without inheriting a fat Manager.
package session

import (
	"context"

	"github.com/andrewhowdencom/ore/loop"
)

// InterceptResult is the result of an Interceptor processing an event.
//
// Event, when non-nil, replaces the original event for downstream
// processing. A nil Event means the original event was consumed and no
// further processing occurs.
//
// Notice carries ephemeral, user-visible messages emitted as
// loop.NoticeEvent after Intercept returns. Each Notice carries a
// Severity that conduits use to pick a rendering style (Success, Info,
// Warn, Error).
type InterceptResult struct {
	Event  Event
	Notice []loop.Notice
}

// Interceptor processes events before they enter the LLM pipeline.
// It receives the session so it can call session-scoped methods like
// SetMetadata, and an emitter for signaling activity.
type Interceptor interface {
	Intercept(ctx context.Context, event Event, sess *Session, emitter loop.Emitter) (InterceptResult, error)
}

// Event is the base interface for ingress events to a Session.
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

// SessionSwitchEvent signals a cross-session navigation. Slash handlers
// emit it to redirect the conduit to another session.
type SessionSwitchEvent struct {
	SessionID string
	Ctx       context.Context
}

// Kind returns the event kind identifier.
func (e SessionSwitchEvent) Kind() string { return "session_switch" }

// Context returns the event's context.Context metadata.
func (e SessionSwitchEvent) Context() context.Context { return e.Ctx }
