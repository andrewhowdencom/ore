// Package export renders Thread conversation histories into
// human-reviewable formats: plain text, self-contained HTML, and JSON.
//
// The package depends only on artifact and ledger. It does not
// import any thread-container type (junk.Thread, session.Session,
// or *ledger.Thread). Callers construct a [Thread] value — a small
// value type carrying just the data the exporters need — from
// whatever container they happen to hold.
//
// All three top-level functions accept an io.Writer and a [Thread],
// iterate over the thread's turns, and emit every artifact in the
// turn. Delta artifacts are never present in persisted threads so
// no special handling is required.
//
// HTML rendering specifics:
//
//   - Text artifact content is rendered as markdown (CommonMark plus
//     the GFM extension for tables, strikethrough, task lists, and
//     autolinks) and sanitized via bluemonday.UGCPolicy. LLM-emitted
//     <script>, <iframe>, and javascript: URLs are stripped.
//   - Reasoning artifacts collapse under a <details> element. Their
//     content is not markdown-rendered — reasoning text is typically
//     stream-of-thought prose, and rendering it risks unintended
//     visual changes (lists appearing where the model was just
//     thinking in prose).
//   - Paired ToolCall + ToolResult artifacts in the same turn
//     collapse under a single <details> with the tool name as the
//     summary. Unpaired ones each get their own <details>.
//
// JSON output uses the wire format `{id, current_tip, metadata,
// turns}`. This format is documented in the workshop README as
// user-facing; it must remain stable across versions so external
// tooling can consume it.
package export

import "github.com/andrewhowdencom/ore/ledger"

// Thread is the minimum data the exporters need from any
// thread-like container. Callers construct one from a *junk.Thread,
// *session.Session, *ledger.Thread, or any in-memory representation
// — the package is intentionally agnostic to the source.
//
// The value type (rather than an interface) is chosen because a
// leaf utility package benefits from cheap value semantics and
// clear field-level access, with no need for the caller to
// implement a method set.
type Thread struct {
	// ID is the thread's identifier. Surfaced in the text header
	// ("Thread: <id>") and the HTML <title>.
	ID string
	// Metadata is arbitrary per-conversation key-value state (for
	// example, "slack_thread_id", "persona"). Surfaced in the
	// text/HTML outputs and the JSON wire format's "metadata"
	// field.
	Metadata map[string]string
	// Turns is the thread's active-path turn list. Callers usually
	// obtain this via *ledger.Thread.Turns(); for in-memory tests
	// it is constructed by hand.
	Turns []ledger.Turn
}