package tool

import (
	"context"
	"errors"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandler_IgnoresNonToolCall(t *testing.T) {
	r := NewRegistry()
	h := r.Handler()
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	err := h.Handle(context.Background(), artifact.Text{Content: "world"}, mem)
	require.NoError(t, err)
	assert.Len(t, mem.Turns(), 1) // No new turns appended.
}

func TestHandler_UnknownTool(t *testing.T) {
	r := NewRegistry()
	h := r.Handler()
	mem := &state.Buffer{}
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
	require.NoError(t, r.Register("add", "Add two numbers", map[string]any{"type": "object"}, func(ctx context.Context, args map[string]any) (any, error) {
		a, _ := args["a"].(float64)
		b, _ := args["b"].(float64)
		return a + b, nil
	}))
	h := r.Handler()
	mem := &state.Buffer{}
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
	require.NoError(t, r.Register("add", "", nil, func(ctx context.Context, args map[string]any) (any, error) {
		return nil, nil
	}))
	h := r.Handler()
	mem := &state.Buffer{}
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
	require.NoError(t, r.Register("fail", "", nil, func(ctx context.Context, args map[string]any) (any, error) {
		return nil, errors.New("boom")
	}))
	h := r.Handler()
	mem := &state.Buffer{}
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
	require.NoError(t, r.Register("bad", "", nil, func(ctx context.Context, args map[string]any) (any, error) {
		// Return a channel, which cannot be JSON-serialized.
		return make(chan int), nil
	}))
	h := r.Handler()
	mem := &state.Buffer{}
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
	require.NoError(t, r.Register("noop", "", nil, func(ctx context.Context, args map[string]any) (any, error) {
		return "done", nil
	}))
	h := r.Handler()
	mem := &state.Buffer{}
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

func TestHandler_NamespacedTool(t *testing.T) {
	remote := &mockRemoteSource{
		name: "filesystem",
		tools: []provider.Tool{
			{Name: "read_file", Description: "Read a file", Schema: map[string]any{"type": "object"}},
		},
	}

	r := NewRegistry(WithMCPServer(remote))
	h := r.Handler()
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	err := h.Handle(context.Background(), artifact.ToolCall{
		ID:        "call_1",
		Name:      "filesystem/read_file",
		Arguments: `{"path": "/tmp/test"}`,
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
	assert.Equal(t, `"remote-result"`, tr.Content)
}

func TestHandler_NamespacedUnknownNamespace(t *testing.T) {
	r := NewRegistry()
	h := r.Handler()
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	err := h.Handle(context.Background(), artifact.ToolCall{
		ID:   "call_1",
		Name: "unknown/read_file",
	}, mem)
	require.NoError(t, err)

	turns := mem.Turns()
	require.Len(t, turns, 2)
	tr, ok := turns[1].Artifacts[0].(artifact.ToolResult)
	require.True(t, ok)
	assert.Equal(t, "call_1", tr.ToolCallID)
	assert.True(t, tr.IsError)
	assert.Contains(t, tr.Content, "namespace")
}

type errorRemoteSource struct{}

func (e *errorRemoteSource) Name() string { return "remote" }
func (e *errorRemoteSource) Tools() []provider.Tool {
	return []provider.Tool{{Name: "fail", Description: "Always fails"}}
}
func (e *errorRemoteSource) Call(ctx context.Context, name string, args map[string]any) (any, error) {
	return nil, errors.New("remote tool failed")
}

func TestHandler_NamespacedRemoteError(t *testing.T) {
	r := NewRegistry(WithMCPServer(&errorRemoteSource{}))
	h := r.Handler()
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	err := h.Handle(context.Background(), artifact.ToolCall{
		ID:        "call_1",
		Name:      "remote/fail",
		Arguments: `{}`,
	}, mem)
	require.NoError(t, err)

	turns := mem.Turns()
	require.Len(t, turns, 2)
	tr, ok := turns[1].Artifacts[0].(artifact.ToolResult)
	require.True(t, ok)
	assert.Equal(t, "call_1", tr.ToolCallID)
	assert.True(t, tr.IsError)
	assert.Contains(t, tr.Content, "tool execution error")
	assert.Contains(t, tr.Content, "remote tool failed")
}

func TestSplitNamespace(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		wantNamespace   string
		wantToolName    string
		wantOk          bool
	}{
		{"standard", "filesystem/read_file", "filesystem", "read_file", true},
		{"nested path", "a/b/c", "a", "b/c", true},
		{"no slash", "tool", "", "", false},
		{"empty string", "", "", "", false},
		{"leading slash", "/tool", "", "tool", true},
		{"trailing slash", "ns/", "ns", "", true},
		{"multiple slashes", "ns/sub/tool", "ns", "sub/tool", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ns, toolName, ok := splitNamespace(tt.input)
			assert.Equal(t, tt.wantOk, ok)
			assert.Equal(t, tt.wantNamespace, ns)
			assert.Equal(t, tt.wantToolName, toolName)
		})
	}
}
