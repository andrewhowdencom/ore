package calculator

import (
	"context"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/state"
	"github.com/andrewhowdencom/ore/tool"
	xtool "github.com/andrewhowdencom/ore/x/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testEmitter wraps a state.Buffer and implements loop.Emitter so that
// TurnCompleteEvents emitted by the handler are persisted into state.
type testEmitter struct {
	buf *state.Buffer
}

func (e *testEmitter) Emit(ctx context.Context, event loop.OutputEvent) {
	if tc, ok := event.(loop.TurnCompleteEvent); ok {
		e.buf.Append(tc.Turn.Role, tc.Turn.Artifacts...)
	}
}

func TestHandler_Add(t *testing.T) {
	registry := tool.NewRegistry()
	require.NoError(t, registry.Register(AddTool, Add))
	handler := xtool.NewHandler(registry)

	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "What is 2 + 3?"})

	err := handler.Handle(context.Background(), artifact.ToolCall{
		ID:        "call_1",
		Name:      "add",
		Arguments: `{"a": 2, "b": 3}`,
	}, &testEmitter{buf: mem})
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
	require.NoError(t, registry.Register(MultiplyTool, Multiply))
	handler := xtool.NewHandler(registry)

	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "What is 4 * 5?"})

	err := handler.Handle(context.Background(), artifact.ToolCall{
		ID:        "call_2",
		Name:      "multiply",
		Arguments: `{"a": 4, "b": 5}`,
	}, &testEmitter{buf: mem})
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
	require.NoError(t, registry.Register(AddTool, Add))
	handler := xtool.NewHandler(registry)

	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "What is 10 / 2?"})

	err := handler.Handle(context.Background(), artifact.ToolCall{
		ID:        "call_3",
		Name:      "divide",
		Arguments: `{"a": 10, "b": 2}`,
	}, &testEmitter{buf: mem})
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
