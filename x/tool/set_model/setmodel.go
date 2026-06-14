// Package set_model provides a slash command that sets the model used for
// inference on the current session. The command is slash-only — there is
// no Tool() function or ToolDescriptor — because the LLM must not be able
// to change its own model.
//
// The model name is written to Thread.Metadata["provider.model"] via
// session.Stream.SetMetadata, which is the framework contract read by
// stream.ModelOption() (and ultimately consumed by provider adapters that
// honor provider.ModelOption, e.g. the OpenAI adapter). SetMetadata also
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

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/x/slash"
)

// metadataKey is the framework contract key consumed by stream.ModelOption.
// It is intentionally re-declared locally rather than exported from the
// session package, matching the private contract documented in PR #436.
const metadataKey = "provider.model"

// usageFeedback is returned when the caller omits the model argument or
// supplies only whitespace. Centralised so the slash and tool paths (the
// latter doesn't exist today but might be added later) can return the same
// message without copy-paste drift.
var usageFeedback = artifact.Text{Content: "Usage: /model <name>"}

// Slash returns a slash.Handler that sets the model on the current
// session's thread metadata. When the trimmed input is empty, the handler
// returns Result.Feedback with usage information and no state change. When
// the input is non-empty, the handler calls SetMetadata (which atomically
// writes Thread.Metadata["provider.model"] and emits a loop.PropertiesEvent
// for UI subscribers).
//
// The handler is nil-safe: a slash command parsed in a context where no
// *session.Stream is available (e.g. unit tests that exercise the registry
// directly) returns the usage feedback instead of panicking. The framework
// guarantees a non-nil stream for handlers running inside session.processOne.
func Slash() slash.Handler {
	return func(ctx context.Context, emitter loop.Emitter, cmd slash.Command) (slash.Result, error) {
		name := strings.TrimSpace(cmd.Input)
		if name == "" {
			return slash.Result{Feedback: usageFeedback}, nil
		}

		stream := cmd.Stream()
		if stream == nil {
			// Defensive: this should not happen when the slash registry
			// is wired via session.WithInterceptor, but a custom host
			// could invoke the handler outside the session pipeline. Return
			// the usage feedback rather than panicking so the user gets a
			// sensible error.
			return slash.Result{
				Feedback: artifact.Text{
					Content: fmt.Sprintf("%s (no active session)", usageFeedback.Content),
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
