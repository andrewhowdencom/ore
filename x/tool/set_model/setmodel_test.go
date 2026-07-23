package set_model

import (
	"context"
	"testing"

	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/x/slash"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockEmitter records every event emitted through the Emitter interface so
// tests can assert that the slash handler does (or does not) emit events
// directly. SetMetadata also emits its own PropertiesEvent; tests that
// assert "no event was emitted" should target paths that return early
// (empty input, nil session).
type mockEmitter struct {
	events []loop.OutputEvent
}

func (m *mockEmitter) Emit(_ context.Context, e loop.OutputEvent) {
	m.events = append(m.events, e)
}

func TestSlash_EmptyInput_ReturnsNotice(t *testing.T) {
	t.Parallel()

	emitter := &mockEmitter{}
	handler := Slash()

	result, err := handler(context.Background(), emitter, slash.Command{Name: "model", Input: ""})
	require.NoError(t, err)
	assert.Equal(t, "Usage: /model <name>", result.Notice.Content)
	assert.Empty(t, emitter.events, "no events should be emitted on empty input")
}

func TestSlash_WhitespaceInput_ReturnsNotice(t *testing.T) {
	t.Parallel()

	emitter := &mockEmitter{}
	handler := Slash()

	result, err := handler(context.Background(), emitter, slash.Command{Name: "model", Input: "   \t  "})
	require.NoError(t, err)
	assert.Equal(t, "Usage: /model <name>", result.Notice.Content)
	assert.Empty(t, emitter.events, "no events should be emitted on whitespace-only input")
}

func TestSlash_TrimsInput(t *testing.T) {
	t.Parallel()

	sess := newMockSession(t)
	emitter := &mockEmitter{}
	handler := Slash()

	// Verify trimming: leading and trailing whitespace is stripped before
	// being stored in metadata. This avoids a common bug where "gpt-4o-mini "
	// is persisted with the trailing space and the OpenAI adapter rejects
	// the model name.
	cmd := slash.NewCommandForTest("model", "  gpt-4o-mini  ", sess)
	result, err := handler(context.Background(), emitter, cmd)
	require.NoError(t, err)
	assert.Empty(t, result.Notice.Content)

	got, ok := sess.GetMetadata("ore.model.name")
	require.True(t, ok)
	assert.Equal(t, "gpt-4o-mini", got)
}

func TestSlash_ValidInput_SetsMetadata(t *testing.T) {
	t.Parallel()

	sess := newMockSession(t)
	emitter := &mockEmitter{}
	handler := Slash()

	cmd := slash.NewCommandForTest("model", "gpt-4o-mini", sess)
	result, err := handler(context.Background(), emitter, cmd)
	require.NoError(t, err)
	assert.Empty(t, result.Notice.Content, "no notice on valid input")

	got, ok := sess.GetMetadata("ore.model.name")
	require.True(t, ok, "metadata should be set after /model succeeds")
	assert.Equal(t, "gpt-4o-mini", got)

	// The handler itself does not emit any event directly; SetMetadata
	// handles emission. The emitter argument is consumed by the handler
	// signature but should not be used by this package.
	assert.Empty(t, emitter.events, "the slash handler must not emit events directly; SetMetadata handles emission")
}

func TestSlash_NilSession_ReturnsNotice(t *testing.T) {
	t.Parallel()

	emitter := &mockEmitter{}
	handler := Slash()

	// Hand-constructed Command with no session: the handler must not panic.
	// It returns an extended usage message instead.
	result, err := handler(context.Background(), emitter, slash.Command{
		Name:  "model",
		Input: "gpt-4o-mini",
		// session is nil here.
	})
	require.NoError(t, err)
	require.NotEmpty(t, result.Notice.Content)
	assert.Contains(t, result.Notice.Content, "Usage: /model <name>")
	assert.Contains(t, result.Notice.Content, "no active session")
	// The "no active session" path warns (recovery is possible on the
	// user's next session); the empty-input path is purely informational.
	assert.Equal(t, loop.SeverityWarn, result.Notice.Severity)
}

func TestSlash_ImplementsSlashHandler(t *testing.T) {
	t.Parallel()

	var _ slash.Handler = Slash()
}