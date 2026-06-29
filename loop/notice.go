package loop

import (
	"context"
	"encoding/json"
	"fmt"
)

// Severity ranks the urgency of a Notice. The zero value is SeveritySuccess
// so that a zero-value Notice renders as a success-styled message — this
// matches the optimistic default for ephemeral feedback and avoids an
// accidental "Error" label from a forgotten severity assignment.
type Severity uint8

// Severity values, in order from least to most urgent. They are intentionally
// typed as uint8 so that additional levels can be appended without breaking
// existing serialised values.
const (
	// SeveritySuccess indicates an operation completed and the message is
	// positive feedback for the user.
	SeveritySuccess Severity = iota
	// SeverityInfo indicates neutral, informational content such as a
	// help listing or a configuration acknowledgement.
	SeverityInfo
	// SeverityWarn indicates a recoverable problem or a degraded result
	// (e.g. a compaction that produced a truncated summary).
	SeverityWarn
	// SeverityError indicates a failure that the user should know about.
	// Slash handler errors are auto-converted into Error-severity Notices
	// at the interceptor boundary so they reach conduits instead of being
	// silently swallowed.
	SeverityError
)

// String returns the human-readable label for the severity. Unknown values
// fall back to a numeric representation so the value remains debuggable
// rather than rendering as an empty string.
func (s Severity) String() string {
	switch s {
	case SeveritySuccess:
		return "Success"
	case SeverityInfo:
		return "Info"
	case SeverityWarn:
		return "Warn"
	case SeverityError:
		return "Error"
	default:
		return fmt.Sprintf("Severity(%d)", uint8(s))
	}
}

// Notice is an ephemeral, user-visible message that is rendered by conduits
// but is not persisted to state and is never sent to the LLM. Notices are
// the framework's single primitive for both success feedback and
// error reporting from non-inference code paths (slash commands, role
// handoffs, system-level confirmations).
//
// See also: ledger.RoleSystem — a turn that the LLM *must* see (role handoffs,
// compaction summaries). Notices are user-only; RoleSystem turns reach both
// the user and the model.
type Notice struct {
	Content  string
	Severity Severity
}

// NoticeEvent is emitted when a Notice needs to flow to conduits. It is
// excluded from state persistence by virtue of its type: the EventBus
// only auto-appends TurnCompleteEvent to bound ledger.
//
// See also: loop.ErrorEvent, which carries inference-layer failures.
// NoticeEvent carries slash-layer and other non-inference feedback; the
// two share a "do not persist" contract but address different sources.
type NoticeEvent struct {
	Notice Notice

	// Ctx carries routing metadata for the event, such as provenance
	// information for echo suppression.
	Ctx context.Context
}

// Kind returns the event kind identifier.
func (e NoticeEvent) Kind() string { return "notice" }

// Context returns the event context.
func (e NoticeEvent) Context() context.Context { return e.Ctx }

// MarshalJSON serialises the event to JSON. The shape is:
//
//	{ "kind": "notice", "content": "...", "severity": "...", "context": {...} }
//
// Conduits use `severity` to pick a rendering style; downstream consumers
// (chat.js, log scrapers) should ignore unknown severities and default
// to the Info style.
func (e NoticeEvent) MarshalJSON() ([]byte, error) {
	type output struct {
		Kind     string                 `json:"kind"`
		Content  string                 `json:"content"`
		Severity string                 `json:"severity"`
		Context  map[string]interface{} `json:"context,omitempty"`
	}
	o := output{
		Kind:     "notice",
		Content:  e.Notice.Content,
		Severity: e.Notice.Severity.String(),
	}
	if ctx := marshalEventContext(e.Ctx); ctx != nil {
		o.Context = ctx
	}
	return json.Marshal(o)
}
