// Package set_model provides a slash command that sets the model used for
// inference on the current session. The command is slash-only — there is
// no Tool() function or ToolDescriptor — because the LLM must not be able
// to change its own model.
//
// The model name is written to Thread.Metadata via
// session.Stream.SetMetadata, using the framework contract key
// "ore.model.name" defined in the session package. The session's Spec()
// method reads that key (and other "ore.model.*" keys) to construct a
// models.Spec that the loop uses for the next turn. SetMetadata also
// emits a loop.PropertiesEvent so UI conduits can react to the change.
//
// Usage:
//
//	/model gpt-4o-mini   → set the model for the rest of the session
//	/model               → reply with usage feedback, no state change
//
// To clear the override, the user closes and reopens the session.
package set_model

import (
	"context"
	"fmt"
	"strings"

	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/x/slash"
)

// metadataKey is the framework contract key consumed by stream.Spec().
// It is re-declared here rather than imported from the session package
// to keep this package free of a session dependency; the session
// package's MetadataKeyModelName constant holds the same value.
const metadataKey = "ore.model.name"

// usageNotice is returned when the caller omits the model argument or
// supplies only whitespace. Centralised so the slash and tool paths (the
// latter doesn't exist today but might be added later) can return the same
// message without copy-paste drift. Info severity matches the other
// informational notices (e.g. /help) so the renderer picks the neutral
// style.
var usageNotice = loop.Notice{Content: "Usage: /model <name>", Severity: loop.SeverityInfo}

// Slash returns a slash.Handler that sets the model on the current
// session's thread metadata. When the trimmed input is empty, the handler
// returns Result.Notice with usage information and no state change. When
// the input is non-empty, the handler calls SetMetadata (which atomically
// writes Thread.Metadata["provider.model"] and emits a loop.PropertiesEvent
// for UI subscribers).
//
// The handler is nil-safe: a slash command parsed in a context where no
// *session.Stream is available (e.g. unit tests that exercise the registry
// directly) returns the usage notice instead of panicking. The framework
// guarantees a non-nil stream for handlers running inside session.processOne.
func Slash() slash.Handler {
	return func(ctx context.Context, emitter loop.Emitter, cmd slash.Command) (slash.Result, error) {
		name := strings.TrimSpace(cmd.Input)
		if name == "" {
			return slash.Result{Notice: usageNotice}, nil
		}

		stream := cmd.Stream()
		if stream == nil {
			// Defensive: this should not happen when the slash registry
			// is wired via session.WithInterceptor, but a custom host
			// could invoke the handler outside the session pipeline. Return
			// the usage notice rather than panicking so the user gets a
			// sensible error. The "no active session" suffix is a warning
			// because the slash interceptor was unable to resolve the
			// stream — the user's instruction may still be valid for
			// their next session.
			return slash.Result{
				Notice: loop.Notice{
					Content:  fmt.Sprintf("%s (no active session)", usageNotice.Content),
					Severity: loop.SeverityWarn,
				},
			}, nil
		}

		// SetMetadata is the canonical write path: it stores the value in
		// Thread.Metadata and emits a loop.PropertiesEvent for subscribers
		// (TUI status bar, HTTP web UI, etc.). The emitter argument is
		// intentionally unused here because SetMetadata handles emission
		// itself; we accept it to satisfy the slash.Handler signature and
		// to keep the door open for future per-handler emissions.
		_ = emitter
		stream.SetMetadata(metadataKey, name)
		return slash.Result{}, nil
	}
}
