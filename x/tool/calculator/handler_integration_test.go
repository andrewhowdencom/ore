package calculator

import (
	"context"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/state"
	"github.com/andrewhowdencom/ore/x/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandler_Add(t *testing.T) {
	registry := tool.NewRegistry()
	require.NoError(t, registry.Register(AddTool.Name, AddTool.Description, AddTool.Schema, Add))
	handler := registry.Handler()

	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "What is 2 + 3?"})

	err := handler.Handle(context.Background(), artifact.ToolCall{
		ID:        "call_1",
		Name:      "add",
		Arguments: `{"a": 2, "b": 3}`,
	}, mem)
	require.NoError(t, err)

	turns := mem.Turns()
	require.Len(t, turns, 2)
	assert.Equal(t, state.RoleTool, turns[1].Role)
	require.Len(t, turns[1].Artifacts, 1)

	tr, ok := turns[1].Artifacts[0].(artifact.ToolResult)
	require.True(t, ok)
	assert.Equal(t, "call_1", tr.ToolCallID)
	assert.False(t, tr.IsError)
	assert.Equal(t, "5", tr.Content)
}

func TestHandler_Multiply(t *testing.T) {
	registry := tool.NewRegistry()
	require.NoError(t, registry.Register(MultiplyTool.Name, MultiplyTool.Description, MultiplyTool.Schema, Multiply))
	handler := registry.Handler()

	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "What is 4 * 5?"})

	err := handler.Handle(context.Background(), artifact.ToolCall{
		ID:        "call_2",
		Name:      "multiply",
		Arguments: `{"a": 4, "b": 5}`,
	}, mem)
	require.NoError(t, err)

	turns := mem.Turns()
	require.Len(t, turns, 2)
	assert.Equal(t, state.RoleTool, turns[1].Role)
	require.Len(t, turns[1].Artifacts, 1)

	tr, ok := turns[1].Artifacts[0].(artifact.ToolResult)
	require.True(t, ok)
	assert.Equal(t, "call_2", tr.ToolCallID)
	assert.False(t, tr.IsError)
	assert.Equal(t, "20", tr.Content)
}

func TestHandler_UnknownTool(t *testing.T) {
	registry := tool.NewRegistry()
	require.NoError(t, registry.Register(AddTool.Name, AddTool.Description, AddTool.Schema, Add))
	handler := registry.Handler()

	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "What is 10 / 2?"})

	err := handler.Handle(context.Background(), artifact.ToolCall{
		ID:        "call_3",
		Name:      "divide",
		Arguments: `{"a": 10, "b": 2}`,
	}, mem)
	require.NoError(t, err)

	turns := mem.Turns()
	require.Len(t, turns, 2)
	assert.Equal(t, state.RoleTool, turns[1].Role)
	require.Len(t, turns[1].Artifacts, 1)

	tr, ok := turns[1].Artifacts[0].(artifact.ToolResult)
	require.True(t, ok)
	assert.Equal(t, "call_3", tr.ToolCallID)
	assert.True(t, tr.IsError)
	assert.Contains(t, tr.Content, "not found")
}
