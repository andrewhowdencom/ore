package request

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/x/slash"
)

// Handler returns a slash.Handler that arms the given dumper for the
// active thread when invoked with the "request" subcommand. The slash
// registry parses "/debug request" into command="debug" with
// input="request"; this handler is the dispatcher for the "debug"
// command and only the "request" subcommand is currently implemented.
//
// The returned handler is safe to invoke concurrently: each call
// arms only the thread of the stream it was dispatched against.
//
// Feedback text:
//
//   - "/debug request" (first arm): "Will dump next request to <path>"
//   - "/debug request" (re-arm):    "Already armed; next request will be captured to <path>"
//   - "/debug" (no subcommand):     "debug: missing subcommand (try /debug request)"
//   - "/debug <unknown>":           "debug: unknown subcommand: <X>"
//
// where <path> is the actual file path the dumper would write on the
// next armed round trip. If the command has no associated stream
// (e.g. hand-constructed in tests without going through the
// registry), the handler returns a "no active session" feedback so
// the caller can surface it.
func Handler(d *Dumper) slash.Handler {
	return func(ctx context.Context, emitter loop.Emitter, cmd slash.Command) (slash.Result, error) {
		// Future debug subcommands (state, tools, ...) dispatch here.
		// For now only "request" is implemented.
		subcommand := strings.TrimSpace(cmd.Input)

		if subcommand == "" {
			return slash.Result{
				Feedback: artifact.Text{Content: "debug: missing subcommand (try /debug request)"},
			}, nil
		}
		if subcommand != "request" {
			return slash.Result{
				Feedback: artifact.Text{
					Content: fmt.Sprintf("debug: unknown subcommand: %s (try /debug request)", subcommand),
				},
			}, nil
		}

		stream := cmd.Stream()
		if stream == nil {
			return slash.Result{
				Feedback: artifact.Text{Content: "debug request: no active session; this command must be run from inside a session"},
			}, nil
		}
		threadID := stream.ID()

		path := d.CapturePath(threadID)

		if d.Armed(threadID) {
			return slash.Result{
				Feedback: artifact.Text{
					Content: fmt.Sprintf("Already armed; next request will be captured to %s", path),
				},
			}, nil
		}

		d.Enable(threadID)
		return slash.Result{
			Feedback: artifact.Text{
				Content: fmt.Sprintf("Will dump next request to %s", path),
			},
		}, nil
	}
}

// Bind registers the /debug command with reg. It is a thin convenience
// over:
//
//	reg.Bind("debug", "Debug subcommands (try /debug request)", Handler(d))
//
// Applications wiring the dumper into their slash registry should
// call Bind exactly once at startup. Other debug subcommands
// (state, tools, ...) will live in sibling x/debug/<name> packages
// and may register their own Bind helpers that share the "debug"
// name with a separate Handler.
func Bind(reg slash.Registry, d *Dumper) {
	reg.Bind("debug", "Debug subcommands (try /debug request)", Handler(d))
}

// CapturePath returns the on-disk path the dumper would write for
// the next armed round trip from the given thread. The path follows
// the convention:
//
//	<outputDir>/<appName>.request.<RFC3339>.log
//
// with ':' replaced by '-' for portability. The timestamp is fixed
// at the time of the call, so callers (in particular the slash
// handler) can present a stable destination in feedback before
// Enable has been called.
//
// This call does not open or create the file; it only synthesizes
// the path. The actual file is created lazily on the next armed
// RoundTrip.
func (d *Dumper) CapturePath(threadID string) string {
	_ = threadID
	stamp := time.Now().UTC().Format(time.RFC3339)
	stamp = strings.ReplaceAll(stamp, ":", "-")
	name := fmt.Sprintf("%s.request.%s.log", d.appName, stamp)
	return filepath.Join(d.outputDir, name)
}