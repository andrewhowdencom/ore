package settitle

import (
	"context"
	"testing"

	"github.com/andrewhowdencom/ore/tool"
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
