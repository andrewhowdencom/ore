package tool

import (
	"context"
	"errors"
	"testing"

	"github.com/andrewhowdencom/tack/artifact"
	"github.com/andrewhowdencom/tack/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandler_IgnoresNonToolCall(t *testing.T) {
	r := NewRegistry()
	h := r.Handler()
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	err := h.Handle(context.Background(), artifact.Text{Content: "world"}, mem)
	require.NoError(t, err)
	assert.Len(t, mem.Turns(), 1) // No new turns appended.
}

func TestHandler_UnknownTool(t *testing.T) {
	r := NewRegistry()
	h := r.Handler()
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	err := h.Handle(context.Background(), artifact.ToolCall{
		ID:   "call_1",
		Name: "unknown",
	}, mem)
	require.NoError(t, err)

	turns := mem.Turns()
	require.Len(t, turns, 2)
	assert.Equal(t, state.RoleTool, turns[1].Role)
	require.Len(t, turns[1].Artifacts, 1)
	tr, ok := turns[1].Artifacts[0].(artifact.ToolResult)
	require.True(t, ok)
	assert.Equal(t, "call_1", tr.ToolCallID)
	assert.True(t, tr.IsError)
	assert.Contains(t, tr.Content, "not found")
}

func TestHandler_ExecutesRegisteredTool(t *testing.T) {
	r := NewRegistry()
	r.Register("add", func(ctx context.Context, args map[string]any) (any, error) {
		a, _ := args["a"].(float64)
		b, _ := args["b"].(float64)
		return a + b, nil
	})
	h := r.Handler()
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	err := h.Handle(context.Background(), artifact.ToolCall{
		ID:        "call_1",
		Name:      "add",
		Arguments: `{"a": 3, "b": 5}`,
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
	assert.Equal(t, "8", tr.Content)
}

func TestHandler_InvalidArguments(t *testing.T) {
	r := NewRegistry()
	r.Register("add", func(ctx context.Context, args map[string]any) (any, error) {
		return nil, nil
	})
	h := r.Handler()
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	err := h.Handle(context.Background(), artifact.ToolCall{
		ID:        "call_1",
		Name:      "add",
		Arguments: `not json`,
	}, mem)
	require.NoError(t, err)

	turns := mem.Turns()
	require.Len(t, turns, 2)
	tr, ok := turns[1].Artifacts[0].(artifact.ToolResult)
	require.True(t, ok)
	assert.True(t, tr.IsError)
	assert.Contains(t, tr.Content, "invalid tool arguments")
}

func TestHandler_ToolExecutionError(t *testing.T) {
	r := NewRegistry()
	r.Register("fail", func(ctx context.Context, args map[string]any) (any, error) {
		return nil, errors.New("boom")
	})
	h := r.Handler()
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	err := h.Handle(context.Background(), artifact.ToolCall{
		ID:        "call_1",
		Name:      "fail",
		Arguments: `{}`,
	}, mem)
	require.NoError(t, err)

	turns := mem.Turns()
	require.Len(t, turns, 2)
	tr, ok := turns[1].Artifacts[0].(artifact.ToolResult)
	require.True(t, ok)
	assert.True(t, tr.IsError)
	assert.Contains(t, tr.Content, "tool execution error")
}

func TestHandler_SerializationError(t *testing.T) {
	r := NewRegistry()
	r.Register("bad", func(ctx context.Context, args map[string]any) (any, error) {
		// Return a channel, which cannot be JSON-serialized.
		return make(chan int), nil
	})
	h := r.Handler()
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	err := h.Handle(context.Background(), artifact.ToolCall{
		ID:        "call_1",
		Name:      "bad",
		Arguments: `{}`,
	}, mem)
	require.NoError(t, err)

	turns := mem.Turns()
	require.Len(t, turns, 2)
	tr, ok := turns[1].Artifacts[0].(artifact.ToolResult)
	require.True(t, ok)
	assert.True(t, tr.IsError)
	assert.Contains(t, tr.Content, "failed to serialize result")
}

func TestHandler_EmptyArguments(t *testing.T) {
	r := NewRegistry()
	r.Register("noop", func(ctx context.Context, args map[string]any) (any, error) {
		return "done", nil
	})
	h := r.Handler()
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	err := h.Handle(context.Background(), artifact.ToolCall{
		ID:   "call_1",
		Name: "noop",
	}, mem)
	require.NoError(t, err)

	turns := mem.Turns()
	require.Len(t, turns, 2)
	tr, ok := turns[1].Artifacts[0].(artifact.ToolResult)
	require.True(t, ok)
	assert.False(t, tr.IsError)
	assert.Equal(t, `"done"`, tr.Content)
}
