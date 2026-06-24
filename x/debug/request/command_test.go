package request

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/x/slash"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// noopEmitter is a noop loop.Emitter used to satisfy the
// slash.Handler signature in tests. The dumper's handler does not
// emit anything; this is provided only to keep the type system
// happy.
type noopEmitter struct{}

func (noopEmitter) Emit(context.Context, loop.OutputEvent) {}

// newStream returns a *session.Stream with the zero-value id ("").
// The session.Stream struct has an unexported id field, so we
// cannot set it from outside the package. Per-thread id semantics
// are already covered exhaustively by the dumper unit tests in
// dumper_test.go; these slash tests focus on the wiring.
func newStream() *session.Stream {
	return &session.Stream{}
}

// TestHandler_ArmsForThreadID asserts that invoking the handler
// with input "request" arms the dumper for the slash command's
// stream's thread ID and produces a feedback string mentioning the
// destination path.
func TestHandler_ArmsForThreadID(t *testing.T) {
	t.Parallel()

	d := New("testapp", WithOutputDir(t.TempDir()))
	t.Cleanup(func() { _ = d.Close() })

	cmd := slash.NewCommandForTest("debug", "request", newStream())
	handler := Handler(d)
	require.NotNil(t, handler)

	result, err := handler(context.Background(), noopEmitter{}, cmd)
	require.NoError(t, err)

	assert.True(t, d.Armed(""),
		"handler should arm the dumper for the stream's thread ID")
	assert.NotEmpty(t, result.Feedback.Content,
		"handler should produce non-empty feedback")
	assert.Contains(t, result.Feedback.Content, "Will dump next request to",
		"first arm feedback should announce a new capture")
	assert.Contains(t, result.Feedback.Content, ".log",
		"feedback should include the capture file path")
}

// TestHandler_ReArmReturnsDifferentFeedback asserts that re-arming
// while already armed returns the "Already armed" message and does
// not toggle the slot.
func TestHandler_ReArmReturnsDifferentFeedback(t *testing.T) {
	t.Parallel()

	d := New("testapp", WithOutputDir(t.TempDir()))
	t.Cleanup(func() { _ = d.Close() })

	handler := Handler(d)
	stream := newStream()

	// First arm.
	first, err := handler(context.Background(), noopEmitter{},
		slash.NewCommandForTest("debug", "request", stream))
	require.NoError(t, err)
	require.True(t, d.Armed(""))

	// Re-arm.
	second, err := handler(context.Background(), noopEmitter{},
		slash.NewCommandForTest("debug", "request", stream))
	require.NoError(t, err)
	require.True(t, d.Armed(""))

	assert.Contains(t, first.Feedback.Content, "Will dump next request to",
		"first arm should announce a new capture")
	assert.Contains(t, second.Feedback.Content, "Already armed",
		"re-arm should announce that the slot is already armed")
}

// TestHandler_NilStreamReturnsFeedback asserts that a slash.Command
// without an associated stream returns a "no active session"
// feedback rather than panicking.
func TestHandler_NilStreamReturnsFeedback(t *testing.T) {
	t.Parallel()

	d := New("testapp", WithOutputDir(t.TempDir()))
	t.Cleanup(func() { _ = d.Close() })

	handler := Handler(d)
	cmd := slash.Command{Name: "debug", Input: "request"} // stream is nil
	result, err := handler(context.Background(), noopEmitter{}, cmd)
	require.NoError(t, err)
	assert.Contains(t, result.Feedback.Content, "no active session",
		"missing-stream feedback should explain the situation")
}

// TestHandler_UnknownSubcommand asserts that input other than
// "request" produces an "unknown subcommand" feedback without arming.
func TestHandler_UnknownSubcommand(t *testing.T) {
	t.Parallel()

	d := New("testapp", WithOutputDir(t.TempDir()))
	t.Cleanup(func() { _ = d.Close() })

	handler := Handler(d)
	cmd := slash.NewCommandForTest("debug", "state", newStream())
	result, err := handler(context.Background(), noopEmitter{}, cmd)
	require.NoError(t, err)
	assert.Contains(t, result.Feedback.Content, "unknown subcommand",
		"unknown subcommand should produce feedback")
	assert.NotContains(t, result.Feedback.Content, "Will dump",
		"unknown subcommand should not arm the dumper")
}

// TestHandler_MissingSubcommand asserts that /debug without an
// input arg produces a helpful "missing subcommand" feedback.
func TestHandler_MissingSubcommand(t *testing.T) {
	t.Parallel()

	d := New("testapp", WithOutputDir(t.TempDir()))
	t.Cleanup(func() { _ = d.Close() })

	handler := Handler(d)
	cmd := slash.NewCommandForTest("debug", "", newStream())
	result, err := handler(context.Background(), noopEmitter{}, cmd)
	require.NoError(t, err)
	assert.Contains(t, result.Feedback.Content, "missing subcommand",
		"missing subcommand should produce feedback")
}

// TestBind_RegistersHandler asserts that Bind adds the /debug
// command to the registry, and that "/debug request"
// UserMessageEvent is dispatched to it.
func TestBind_RegistersHandler(t *testing.T) {
	t.Parallel()

	d := New("testapp", WithOutputDir(t.TempDir()))
	t.Cleanup(func() { _ = d.Close() })

	reg := slash.NewRegistry()
	Bind(reg, d)

	stream := newStream()
	event := session.UserMessageEvent{Content: "/debug request"}
	result, err := reg.Intercept(context.Background(), event, stream, noopEmitter{})
	require.NoError(t, err)

	assert.True(t, d.Armed(""),
		"the registry-bound handler should arm the dumper for the stream's thread")
	assert.NotEmpty(t, result.Feedback,
		"the registry should surface the handler's feedback")
}

// TestBind_DispatchesUnknownSubcommand asserts that "/debug state"
// routes through our handler (which then rejects it as unknown)
// rather than passing through as an unknown slash command.
func TestBind_DispatchesUnknownSubcommand(t *testing.T) {
	t.Parallel()

	d := New("testapp", WithOutputDir(t.TempDir()))
	t.Cleanup(func() { _ = d.Close() })

	reg := slash.NewRegistry()
	Bind(reg, d)

	stream := newStream()
	event := session.UserMessageEvent{Content: "/debug state"}
	result, err := reg.Intercept(context.Background(), event, stream, noopEmitter{})
	require.NoError(t, err)
	assert.NotEmpty(t, result.Feedback)
	assert.Contains(t, result.Feedback[0].Content, "unknown subcommand",
		"/debug state should be rejected by the debug handler")
}

// TestBind_UnknownCommandPassesThrough asserts that Bind does not
// swallow unrelated slash commands.
func TestBind_UnknownCommandPassesThrough(t *testing.T) {
	t.Parallel()

	d := New("testapp", WithOutputDir(t.TempDir()))
	t.Cleanup(func() { _ = d.Close() })

	reg := slash.NewRegistry()
	Bind(reg, d)

	stream := newStream()
	unknown := session.UserMessageEvent{Content: "/unknown command"}
	result, err := reg.Intercept(context.Background(), unknown, stream, noopEmitter{})
	require.NoError(t, err)
	assert.NotEmpty(t, result.Feedback,
		"unknown command should produce feedback")
	assert.Contains(t, result.Feedback[0].Content, "Unknown command",
		"unknown command feedback should explain the rejection")
}

// TestCapturePath_ConformsToConvention asserts the public
// CapturePath method returns a path matching the documented
// <outputDir>/<appName>.request.<RFC3339>.log pattern.
func TestCapturePath_ConformsToConvention(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	d := New("my-app", WithOutputDir(dir))
	t.Cleanup(func() { _ = d.Close() })

	p := d.CapturePath("any")
	assert.True(t, strings.HasPrefix(p, dir),
		"path=%q should live under %q", p, dir)
	assert.Contains(t, p, filepath.Base(dir)+string(filepath.Separator),
		"path should include the output dir basename")
	assert.Contains(t, p, "my-app.request.")
	assert.Contains(t, p, ".log")
	// RFC3339 substitutes ':' with '-'.
	assert.NotContains(t, p, ":",
		"path should not contain ':' (filesystem portability)")
}

// Unused import guard: artifact is imported because slash.Result.Feedback
// is of type artifact.Text. The session package is needed for
// session.UserMessageEvent construction in the registry tests.
var (
	_ = artifact.Text{}
	_ session.Event = session.UserMessageEvent{}
)