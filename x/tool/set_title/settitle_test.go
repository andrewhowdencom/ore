package set_title

import (
	"context"
	"testing"

	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/tool"
	"github.com/andrewhowdencom/ore/x/slash"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTool_SetsTitle(t *testing.T) {
	t.Parallel()

	result, err := Tool()(context.Background(), nil, map[string]any{"title": "My Chat"})
	require.NoError(t, err)

	update, ok := result.(TitleUpdate)
	require.True(t, ok)
	assert.Equal(t, "My Chat", update.Title)
}

func TestTool_MissingTitle(t *testing.T) {
	t.Parallel()

	_, err := Tool()(context.Background(), nil, map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing or empty")
}

func TestTool_EmptyTitle(t *testing.T) {
	t.Parallel()

	_, err := Tool()(context.Background(), nil, map[string]any{"title": ""})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing or empty")
}

func TestTitleUpdate_Status(t *testing.T) {
	t.Parallel()

	u := TitleUpdate{Title: "Foo"}
	assert.Equal(t, map[string]string{"title": "Foo"}, u.Status())
}

func TestTitleUpdate_ImplementsStatusContributor(t *testing.T) {
	t.Parallel()

	var _ interface{ Status() map[string]string } = TitleUpdate{}
}

func TestTool_ImplementsToolFunc(t *testing.T) {
	t.Parallel()

	var _ tool.ToolFunc = Tool()
}

// mockEmitter records the last event emitted through the Emitter interface
// so slash-handler tests can assert what was sent to the session stream.
type mockEmitter struct {
	last   loop.OutputEvent
	events []loop.OutputEvent
}

func (m *mockEmitter) Emit(ctx context.Context, event loop.OutputEvent) {
	m.last = event
	m.events = append(m.events, event)
}

func TestSlash_EmptyInput_ReturnsFeedback(t *testing.T) {
	t.Parallel()

	emitter := &mockEmitter{}
	handler := Slash()

	result, err := handler(context.Background(), emitter, slash.Command{Name: "name", Input: ""})
	require.NoError(t, err)
	assert.Equal(t, "Usage: /name <text>", result.Feedback.Content)
	assert.Nil(t, result.Replace)
	assert.Empty(t, emitter.events, "no PropertiesEvent should be emitted on empty input")
}

func TestSlash_WhitespaceInput_ReturnsFeedback(t *testing.T) {
	t.Parallel()

	emitter := &mockEmitter{}
	handler := Slash()

	result, err := handler(context.Background(), emitter, slash.Command{Name: "name", Input: "   \t  "})
	require.NoError(t, err)
	assert.Equal(t, "Usage: /name <text>", result.Feedback.Content)
	assert.Nil(t, result.Replace)
	assert.Empty(t, emitter.events, "no PropertiesEvent should be emitted on whitespace-only input")
}

func TestSlash_ValidInput_EmitsPropertiesEvent(t *testing.T) {
	t.Parallel()

	emitter := &mockEmitter{}
	handler := Slash()

	result, err := handler(context.Background(), emitter, slash.Command{Name: "name", Input: "Fix login bug"})
	require.NoError(t, err)
	assert.Empty(t, result.Feedback.Content)
	assert.Nil(t, result.Replace)

	require.Len(t, emitter.events, 1)
	pe, ok := emitter.last.(loop.PropertiesEvent)
	require.True(t, ok, "expected loop.PropertiesEvent, got %T", emitter.last)
	assert.Equal(t, "Fix login bug", pe.Properties["title"])
	assert.Equal(t, context.Background(), pe.Ctx)
}

func TestSlash_TrimsInput(t *testing.T) {
	t.Parallel()

	emitter := &mockEmitter{}
	handler := Slash()

	result, err := handler(context.Background(), emitter, slash.Command{Name: "name", Input: "  spaced  "})
	require.NoError(t, err)
	assert.Empty(t, result.Feedback.Content)

	require.Len(t, emitter.events, 1)
	pe := emitter.last.(loop.PropertiesEvent)
	assert.Equal(t, "spaced", pe.Properties["title"])
}

func TestSlash_ImplementsSlashHandler(t *testing.T) {
	t.Parallel()

	var _ slash.Handler = Slash()
}
