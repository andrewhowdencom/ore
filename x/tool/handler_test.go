package tool

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/ledger"
	toolpkg "github.com/andrewhowdencom/ore/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockEmitter struct {
	events []loop.OutputEvent
}

func (m *mockEmitter) Emit(ctx context.Context, event loop.OutputEvent) {
	m.events = append(m.events, event)
}

func TestHandler_IgnoresNonToolCall(t *testing.T) {
	r := toolpkg.NewRegistry()
	h := NewHandler(r)
	emitter := &mockEmitter{}

	err := h.Handle(context.Background(), artifact.Text{Content: "world"}, emitter)
	require.NoError(t, err)
	assert.Len(t, emitter.events, 0) // No events emitted.
}

func TestHandler_UnknownTool(t *testing.T) {
	r := toolpkg.NewRegistry()
	h := NewHandler(r)
	emitter := &mockEmitter{}

	err := h.Handle(context.Background(), artifact.ToolCall{
		ID:   "call_1",
		Name: "unknown",
	}, emitter)
	require.NoError(t, err)

	require.Len(t, emitter.events, 1)
	tc, ok := emitter.events[0].(loop.TurnCompleteEvent)
	require.True(t, ok)
	assert.Equal(t, ledger.RoleTool, tc.Turn.Role)
	require.Len(t, tc.Turn.Artifacts, 1)
	tr, ok := tc.Turn.Artifacts[0].(artifact.ToolResult)
	require.True(t, ok)
	assert.Equal(t, "call_1", tr.ToolCallID)
	assert.True(t, tr.IsError)
	assert.Contains(t, tr.Content, "not found")
}

func TestHandler_ExecutesRegisteredTool(t *testing.T) {
	r := toolpkg.NewRegistry()
	require.NoError(t, r.Register(toolpkg.Tool{Name: "add", Description: "Add two numbers", Schema: map[string]any{"type": "object"}}, func(ctx context.Context, _ toolpkg.Sandbox, args map[string]any) (any, error) {
		a, _ := args["a"].(float64)
		b, _ := args["b"].(float64)
		return a + b, nil
	}))
	h := NewHandler(r)
	emitter := &mockEmitter{}

	err := h.Handle(context.Background(), artifact.ToolCall{
		ID:        "call_1",
		Name:      "add",
		Arguments: `{"a": 3, "b": 5}`,
	}, emitter)
	require.NoError(t, err)

	require.Len(t, emitter.events, 1)
	tc, ok := emitter.events[0].(loop.TurnCompleteEvent)
	require.True(t, ok)
	assert.Equal(t, ledger.RoleTool, tc.Turn.Role)
	require.Len(t, tc.Turn.Artifacts, 1)
	tr, ok := tc.Turn.Artifacts[0].(artifact.ToolResult)
	require.True(t, ok)
	assert.Equal(t, "call_1", tr.ToolCallID)
	assert.False(t, tr.IsError)
	assert.Equal(t, "8", tr.Content)
	assert.Equal(t, 8.0, tr.Value)

	// Result is a plain float64; no StatusContributor, so no extra event.
	assert.Len(t, emitter.events, 1)
}

func TestHandler_InvalidArguments(t *testing.T) {
	r := toolpkg.NewRegistry()
	require.NoError(t, r.Register(toolpkg.Tool{Name: "add", Description: "", Schema: nil}, func(ctx context.Context, _ toolpkg.Sandbox, args map[string]any) (any, error) {
		return nil, nil
	}))
	h := NewHandler(r)
	emitter := &mockEmitter{}

	err := h.Handle(context.Background(), artifact.ToolCall{
		ID:        "call_1",
		Name:      "add",
		Arguments: `not json`,
	}, emitter)
	require.NoError(t, err)

	require.Len(t, emitter.events, 1)
	tc, ok := emitter.events[0].(loop.TurnCompleteEvent)
	require.True(t, ok)
	require.Len(t, tc.Turn.Artifacts, 1)
	tr, ok := tc.Turn.Artifacts[0].(artifact.ToolResult)
	require.True(t, ok)
	assert.True(t, tr.IsError)
	assert.Contains(t, tr.Content, "invalid tool arguments")
}

func TestHandler_ToolExecutionError_WithResult(t *testing.T) {
	r := toolpkg.NewRegistry()
	require.NoError(t, r.Register(toolpkg.Tool{Name: "fail", Description: "", Schema: nil}, func(ctx context.Context, _ toolpkg.Sandbox, args map[string]any) (any, error) {
		return map[string]any{
			"stdout":    "partial",
			"stderr":    "something failed",
			"exit_code": 1,
		}, errors.New("exit 1")
	}))
	h := NewHandler(r)
	emitter := &mockEmitter{}

	err := h.Handle(context.Background(), artifact.ToolCall{
		ID:        "call_1",
		Name:      "fail",
		Arguments: `{}`,
	}, emitter)
	require.NoError(t, err)

	require.Len(t, emitter.events, 1)
	tc, ok := emitter.events[0].(loop.TurnCompleteEvent)
	require.True(t, ok)
	require.Len(t, tc.Turn.Artifacts, 1)
	tr, ok := tc.Turn.Artifacts[0].(artifact.ToolResult)
	require.True(t, ok)
	assert.True(t, tr.IsError)
	// The partial result is preserved (so the LLM can still see the
	// partial output) and the error is appended as a footer that is
	// exempt from truncation.
	assert.Contains(t, tr.Content, `{"exit_code":1,"stderr":"something failed","stdout":"partial"}`)
	assert.Contains(t, tr.Content, "**Error:** exit 1")
	assert.True(t, strings.HasSuffix(strings.TrimRight(tr.Content, "\n"), "**Error:** exit 1"),
		"error footer must be the last non-whitespace content")
	assert.Equal(t, map[string]any{
		"stdout":    "partial",
		"stderr":    "something failed",
		"exit_code": 1,
	}, tr.Value)
}

func TestHandler_ToolExecutionError(t *testing.T) {
	r := toolpkg.NewRegistry()
	require.NoError(t, r.Register(toolpkg.Tool{Name: "fail", Description: "", Schema: nil}, func(ctx context.Context, _ toolpkg.Sandbox, args map[string]any) (any, error) {
		return nil, errors.New("boom")
	}))
	h := NewHandler(r)
	emitter := &mockEmitter{}

	err := h.Handle(context.Background(), artifact.ToolCall{
		ID:        "call_1",
		Name:      "fail",
		Arguments: `{}`,
	}, emitter)
	require.NoError(t, err)

	require.Len(t, emitter.events, 1)
	tc, ok := emitter.events[0].(loop.TurnCompleteEvent)
	require.True(t, ok)
	require.Len(t, tc.Turn.Artifacts, 1)
	tr, ok := tc.Turn.Artifacts[0].(artifact.ToolResult)
	require.True(t, ok)
	assert.True(t, tr.IsError)
	// With a nil result the handler synthesises a structured body
	// (`{"error": "..."}`) and appends the error footer.
	assert.Contains(t, tr.Content, `{"error":"boom"}`)
	assert.Contains(t, tr.Content, "**Error:** boom")
}

// llmResult is a minimal LLMRenderer used to verify the error path
// honours the renderer's verbatim output (the error footer is then
// appended below the rendered body).
type llmResult struct{ Body string }

func (l llmResult) MarshalLLM() string { return l.Body }

// mdResult is a minimal MarkdownRenderer with the same purpose.
type mdResult struct{ Body string }

func (m mdResult) MarshalMarkdown() string { return m.Body }

func TestHandler_SerializationError(t *testing.T) {
	r := toolpkg.NewRegistry()
	require.NoError(t, r.Register(toolpkg.Tool{Name: "bad", Description: "", Schema: nil}, func(ctx context.Context, _ toolpkg.Sandbox, args map[string]any) (any, error) {
		// Return a channel, which cannot be JSON-serialized.
		return make(chan int), nil
	}))
	h := NewHandler(r)
	emitter := &mockEmitter{}

	err := h.Handle(context.Background(), artifact.ToolCall{
		ID:        "call_1",
		Name:      "bad",
		Arguments: `{}`,
	}, emitter)
	require.NoError(t, err)

	require.Len(t, emitter.events, 1)
	tc, ok := emitter.events[0].(loop.TurnCompleteEvent)
	require.True(t, ok)
	require.Len(t, tc.Turn.Artifacts, 1)
	tr, ok := tc.Turn.Artifacts[0].(artifact.ToolResult)
	require.True(t, ok)
	assert.True(t, tr.IsError)
	assert.Contains(t, tr.Content, "failed to serialize result")
}

// TestHandler_ToolExecutionError_PreservesLLMRenderer verifies that
// when a tool returns an LLMRenderer alongside an error, the renderer's
// verbatim output is preserved as the body of the ToolResult.Content,
// and the error footer is appended below. This is the opt-out path
// that the bash tool uses to take full control of its rendering; the
// fix must not break it on errors.
func TestHandler_ToolExecutionError_PreservesLLMRenderer(t *testing.T) {
	r := toolpkg.NewRegistry()
	require.NoError(t, r.Register(toolpkg.Tool{Name: "render", Description: "", Schema: nil}, func(ctx context.Context, _ toolpkg.Sandbox, args map[string]any) (any, error) {
		return llmResult{Body: "rendered body"}, errors.New("rendered failure")
	}))
	h := NewHandler(r)
	emitter := &mockEmitter{}

	err := h.Handle(context.Background(), artifact.ToolCall{
		ID:        "call_1",
		Name:      "render",
		Arguments: `{}`,
	}, emitter)
	require.NoError(t, err)

	require.Len(t, emitter.events, 1)
	tc, ok := emitter.events[0].(loop.TurnCompleteEvent)
	require.True(t, ok)
	require.Len(t, tc.Turn.Artifacts, 1)
	tr, ok := tc.Turn.Artifacts[0].(artifact.ToolResult)
	require.True(t, ok)
	assert.True(t, tr.IsError)
	// The renderer's verbatim output is the body; the footer is
	// appended after. The footer must be present and visible to
	// the LLM.
	assert.True(t, strings.HasPrefix(tr.Content, "rendered body"),
		"LLMRenderer output should be the body prefix; got %q", tr.Content)
	assert.Contains(t, tr.Content, "**Error:** rendered failure")
}

// TestHandler_ToolExecutionError_HumanViewIncludesFooter verifies that
// the human-facing view of an error result includes the error
// footer. `applyFormat` only honours the `LLMRenderer` opt-out (the
// bash tool's path); `MarkdownRenderer` on Value is bypassed on
// errors in favour of the JSON form. The test pins that contract:
// the human view, regardless of whether Value carries a
// MarkdownRenderer, contains the partial body and the footer.
func TestHandler_ToolExecutionError_HumanViewIncludesFooter(t *testing.T) {
	r := toolpkg.NewRegistry()
	require.NoError(t, r.Register(toolpkg.Tool{Name: "render", Description: "", Schema: nil}, func(ctx context.Context, _ toolpkg.Sandbox, args map[string]any) (any, error) {
		return mdResult{Body: "rendered body"}, errors.New("rendered failure")
	}))
	h := NewHandler(r)
	emitter := &mockEmitter{}

	err := h.Handle(context.Background(), artifact.ToolCall{
		ID:        "call_1",
		Name:      "render",
		Arguments: `{}`,
	}, emitter)
	require.NoError(t, err)

	require.Len(t, emitter.events, 1)
	tc, ok := emitter.events[0].(loop.TurnCompleteEvent)
	require.True(t, ok)
	require.Len(t, tc.Turn.Artifacts, 1)
	tr, ok := tc.Turn.Artifacts[0].(artifact.ToolResult)
	require.True(t, ok)
	assert.True(t, tr.IsError)
	// MarkdownString() is what the human sees. The body is the
	// JSON form (because applyFormat only honours LLMRenderer),
	// but the error footer must always be present.
	md := tr.MarkdownString()
	assert.Contains(t, md, `"Body":"rendered body"`)
	assert.Contains(t, md, "**Error:** rendered failure")
	// The renderer on Value is bypassed in favour of the JSON body
	// on the error path. This is the documented contract of
	// applyFormat: LLMRenderer is the only opt-out.
	assert.NotEqual(t, "rendered body", md,
		"MarkdownRenderer on Value is bypassed on the error path; "+
			"the JSON form is used so the partial body is preserved")
}

// TestHandler_ToolExecutionError_NoResult exercises the (nil, err)
// case: there is no partial result to truncate, so the framework
// synthesises a structured body that carries the error message and
// the footer is appended below.
func TestHandler_ToolExecutionError_NoResult(t *testing.T) {
	r := toolpkg.NewRegistry()
	require.NoError(t, r.Register(toolpkg.Tool{Name: "fail", Description: "", Schema: nil}, func(ctx context.Context, _ toolpkg.Sandbox, args map[string]any) (any, error) {
		return nil, errors.New("nothing came back")
	}))
	h := NewHandler(r)
	emitter := &mockEmitter{}

	err := h.Handle(context.Background(), artifact.ToolCall{
		ID:        "call_1",
		Name:      "fail",
		Arguments: `{}`,
	}, emitter)
	require.NoError(t, err)

	require.Len(t, emitter.events, 1)
	tc, ok := emitter.events[0].(loop.TurnCompleteEvent)
	require.True(t, ok)
	require.Len(t, tc.Turn.Artifacts, 1)
	tr, ok := tc.Turn.Artifacts[0].(artifact.ToolResult)
	require.True(t, ok)
	assert.True(t, tr.IsError)
	// The synthesised body carries the error message so the LLM
	// sees a structured payload, not a blank body.
	assert.Contains(t, tr.Content, `{"error":"nothing came back"}`)
	assert.Contains(t, tr.Content, "**Error:** nothing came back")
	// LLMString (the LLM-facing view) must include the error too.
	assert.Contains(t, tr.LLMString(), "**Error:** nothing came back")
	// The Value is nil because the tool returned nil; the synthetic
	// struct is local to the handler and is not exposed.
	assert.Nil(t, tr.Value)
}

// TestHandler_ToolExecutionError_FooterNotTruncated verifies that
// when a tool's Format sets a MaxBytes cap, the partial result is
// bounded by that cap but the error footer is appended afterwards
// in full. This guards the ordering invariant: the footer must be
// appended AFTER applyFormat returns, not before.
func TestHandler_ToolExecutionError_FooterNotTruncated(t *testing.T) {
	// Build a partial result that is much larger than the cap.
	big := strings.Repeat("x", 4096)
	cap := 64

	r := toolpkg.NewRegistry()
	require.NoError(t, r.Register(toolpkg.Tool{
		Name:        "verbose",
		Description: "",
		Schema:      nil,
		Format: toolpkg.Format{
			Truncate: toolpkg.TruncateConfig{
				MaxBytes: cap,
			},
		},
	}, func(ctx context.Context, _ toolpkg.Sandbox, args map[string]any) (any, error) {
		return big, errors.New("partial run failed")
	}))
	h := NewHandler(r)
	emitter := &mockEmitter{}

	err := h.Handle(context.Background(), artifact.ToolCall{
		ID:        "call_1",
		Name:      "verbose",
		Arguments: `{}`,
	}, emitter)
	require.NoError(t, err)

	require.Len(t, emitter.events, 1)
	tc, ok := emitter.events[0].(loop.TurnCompleteEvent)
	require.True(t, ok)
	require.Len(t, tc.Turn.Artifacts, 1)
	tr, ok := tc.Turn.Artifacts[0].(artifact.ToolResult)
	require.True(t, ok)
	assert.True(t, tr.IsError)
	// Footer is intact, exempt from the cap.
	assert.Contains(t, tr.Content, "**Error:** partial run failed")
	// The body before the footer is bounded; the partial result is
	// not present in its entirety. We check that the body is shorter
	// than the full input by looking at the position of the footer.
	idx := strings.Index(tr.Content, "**Error:**")
	require.GreaterOrEqual(t, idx, 0)
	body := tr.Content[:idx]
	assert.LessOrEqual(t, len(body), cap+32,
		"body before footer should be near the truncation cap, not the full input")
	// And Truncation metadata is set because the body was truncated.
	require.NotNil(t, tr.Truncation)
	assert.True(t, tr.Truncation.Truncated())
}

func TestHandler_EmptyArguments(t *testing.T) {
	r := toolpkg.NewRegistry()
	require.NoError(t, r.Register(toolpkg.Tool{Name: "noop", Description: "", Schema: nil}, func(ctx context.Context, _ toolpkg.Sandbox, args map[string]any) (any, error) {
		return "done", nil
	}))
	h := NewHandler(r)
	emitter := &mockEmitter{}

	err := h.Handle(context.Background(), artifact.ToolCall{
		ID:   "call_1",
		Name: "noop",
	}, emitter)
	require.NoError(t, err)

	require.Len(t, emitter.events, 1)
	tc, ok := emitter.events[0].(loop.TurnCompleteEvent)
	require.True(t, ok)
	require.Len(t, tc.Turn.Artifacts, 1)
	tr, ok := tc.Turn.Artifacts[0].(artifact.ToolResult)
	require.True(t, ok)
	assert.False(t, tr.IsError)
	assert.Equal(t, `"done"`, tr.Content)
	assert.Equal(t, "done", tr.Value)
}

func TestHandler_ArrayReturnValue(t *testing.T) {
	r := toolpkg.NewRegistry()
	require.NoError(t, r.Register(toolpkg.Tool{Name: "list", Description: "", Schema: nil}, func(ctx context.Context, _ toolpkg.Sandbox, args map[string]any) (any, error) {
		return []int{1, 2, 3}, nil
	}))
	h := NewHandler(r)
	emitter := &mockEmitter{}

	err := h.Handle(context.Background(), artifact.ToolCall{
		ID:   "call_1",
		Name: "list",
	}, emitter)
	require.NoError(t, err)

	require.Len(t, emitter.events, 1)
	tc, ok := emitter.events[0].(loop.TurnCompleteEvent)
	require.True(t, ok)
	require.Len(t, tc.Turn.Artifacts, 1)
	tr, ok := tc.Turn.Artifacts[0].(artifact.ToolResult)
	require.True(t, ok)
	assert.False(t, tr.IsError)
	assert.Equal(t, "[1,2,3]", tr.Content)
	assert.Equal(t, []int{1, 2, 3}, tr.Value)
}

func TestHandler_NamespacedTool(t *testing.T) {
	remote := &mockRemoteSource{
		name: "filesystem",
		tools: []toolpkg.Tool{
			{Name: "read_file", Description: "Read a file", Schema: map[string]any{"type": "object"}},
		},
	}

	r := toolpkg.NewRegistry(toolpkg.WithMCPServer(remote))
	h := NewHandler(r)
	emitter := &mockEmitter{}

	err := h.Handle(context.Background(), artifact.ToolCall{
		ID:        "call_1",
		Name:      "filesystem/read_file",
		Arguments: `{"path": "/tmp/test"}`,
	}, emitter)
	require.NoError(t, err)

	require.Len(t, emitter.events, 1)
	tc, ok := emitter.events[0].(loop.TurnCompleteEvent)
	require.True(t, ok)
	assert.Equal(t, ledger.RoleTool, tc.Turn.Role)
	require.Len(t, tc.Turn.Artifacts, 1)
	tr, ok := tc.Turn.Artifacts[0].(artifact.ToolResult)
	require.True(t, ok)
	assert.Equal(t, "call_1", tr.ToolCallID)
	assert.False(t, tr.IsError)
	assert.Equal(t, `"remote-result"`, tr.Content)
	assert.Equal(t, "remote-result", tr.Value)
}

func TestHandler_NamespacedUnknownNamespace(t *testing.T) {
	r := toolpkg.NewRegistry()
	h := NewHandler(r)
	emitter := &mockEmitter{}

	err := h.Handle(context.Background(), artifact.ToolCall{
		ID:   "call_1",
		Name: "unknown/read_file",
	}, emitter)
	require.NoError(t, err)

	require.Len(t, emitter.events, 1)
	tc, ok := emitter.events[0].(loop.TurnCompleteEvent)
	require.True(t, ok)
	require.Len(t, tc.Turn.Artifacts, 1)
	tr, ok := tc.Turn.Artifacts[0].(artifact.ToolResult)
	require.True(t, ok)
	assert.Equal(t, "call_1", tr.ToolCallID)
	assert.True(t, tr.IsError)
	assert.Contains(t, tr.Content, "namespace")
}

type notFoundRemoteSource struct{}

func (e *notFoundRemoteSource) Name() string { return "remote" }
func (e *notFoundRemoteSource) Tools() []toolpkg.Tool {
	return []toolpkg.Tool{{Name: "known", Description: "Known tool"}}
}
func (e *notFoundRemoteSource) Call(ctx context.Context, name string, args map[string]any) (any, error) {
	return nil, fmt.Errorf("tool %q not found", name)
}

type errorRemoteSource struct{}

func (e *errorRemoteSource) Name() string { return "remote" }
func (e *errorRemoteSource) Tools() []toolpkg.Tool {
	return []toolpkg.Tool{{Name: "fail", Description: "Always fails"}}
}
func (e *errorRemoteSource) Call(ctx context.Context, name string, args map[string]any) (any, error) {
	return nil, errors.New("remote tool failed")
}

func TestHandler_NamespacedRemoteError(t *testing.T) {
	r := toolpkg.NewRegistry(toolpkg.WithMCPServer(&errorRemoteSource{}))
	h := NewHandler(r)
	emitter := &mockEmitter{}

	err := h.Handle(context.Background(), artifact.ToolCall{
		ID:        "call_1",
		Name:      "remote/fail",
		Arguments: `{}`,
	}, emitter)
	require.NoError(t, err)

	require.Len(t, emitter.events, 1)
	tc, ok := emitter.events[0].(loop.TurnCompleteEvent)
	require.True(t, ok)
	require.Len(t, tc.Turn.Artifacts, 1)
	tr, ok := tc.Turn.Artifacts[0].(artifact.ToolResult)
	require.True(t, ok)
	assert.Equal(t, "call_1", tr.ToolCallID)
	assert.True(t, tr.IsError)
	// Remote error path: synthesised body plus the error footer.
	assert.Contains(t, tr.Content, `{"error":"remote tool failed"}`)
	assert.Contains(t, tr.Content, "**Error:** remote tool failed")
}

func TestHandler_NamespacedRemoteToolNotFound(t *testing.T) {
	r := toolpkg.NewRegistry(toolpkg.WithMCPServer(&notFoundRemoteSource{}))
	h := NewHandler(r)
	emitter := &mockEmitter{}

	err := h.Handle(context.Background(), artifact.ToolCall{
		ID:   "call_1",
		Name: "remote/unknown",
	}, emitter)
	require.NoError(t, err)

	require.Len(t, emitter.events, 1)
	tc, ok := emitter.events[0].(loop.TurnCompleteEvent)
	require.True(t, ok)
	require.Len(t, tc.Turn.Artifacts, 1)
	tr, ok := tc.Turn.Artifacts[0].(artifact.ToolResult)
	require.True(t, ok)
	assert.Equal(t, "call_1", tr.ToolCallID)
	assert.True(t, tr.IsError)
	// Remote error path when the remote source returned (nil, err).
	// The synthesised body carries the remote error string; the
	// footer is appended.
	assert.Contains(t, tr.Content, `not found`)
	assert.Contains(t, tr.Content, "**Error:**")
}

func TestSplitNamespace(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		wantNamespace string
		wantToolName  string
		wantOk        bool
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

type mockRemoteSource struct {
	name  string
	tools []toolpkg.Tool
}

func (m *mockRemoteSource) Name() string { return m.name }
func (m *mockRemoteSource) Tools() []toolpkg.Tool {
	t := make([]toolpkg.Tool, len(m.tools))
	copy(t, m.tools)
	return t
}
func (m *mockRemoteSource) Call(ctx context.Context, name string, args map[string]any) (any, error) {
	return "remote-result", nil
}

type mockSandbox struct {
	name string
}

func (m *mockSandbox) Name() string { return m.name }

func TestHandler_ResolvesSandboxFromArgs(t *testing.T) {
	r := toolpkg.NewRegistry().(toolpkg.SandboxRegistry)
	var calledWith toolpkg.Sandbox
	require.NoError(t, r.Register(toolpkg.Tool{Name: "check", Description: "", Schema: nil}, func(ctx context.Context, sb toolpkg.Sandbox, args map[string]any) (any, error) {
		calledWith = sb
		return "ok", nil
	}))

	sb := &mockSandbox{name: "worktree"}
	r.RegisterSandbox("worktree", sb)

	h := NewHandler(r)
	emitter := &mockEmitter{}

	err := h.Handle(context.Background(), artifact.ToolCall{
		ID:        "call_1",
		Name:      "check",
		Arguments: `{"sandbox": "worktree"}`,
	}, emitter)
	require.NoError(t, err)

	assert.Equal(t, sb, calledWith)
}

func TestHandler_UsesDefaultSandbox(t *testing.T) {
	r := toolpkg.NewRegistry().(toolpkg.SandboxRegistry)
	var calledWith toolpkg.Sandbox
	require.NoError(t, r.Register(toolpkg.Tool{Name: "check", Description: "", Schema: nil}, func(ctx context.Context, sb toolpkg.Sandbox, args map[string]any) (any, error) {
		calledWith = sb
		return "ok", nil
	}))

	defaultSb := &mockSandbox{name: "default"}
	r.SetDefaultSandbox(defaultSb)

	h := NewHandler(r)
	emitter := &mockEmitter{}

	err := h.Handle(context.Background(), artifact.ToolCall{
		ID:        "call_1",
		Name:      "check",
		Arguments: `{}`,
	}, emitter)
	require.NoError(t, err)

	assert.Equal(t, defaultSb, calledWith)
}

func TestHandler_StripsSandboxArg(t *testing.T) {
	r := toolpkg.NewRegistry().(toolpkg.SandboxRegistry)
	var receivedArgs map[string]any
	require.NoError(t, r.Register(toolpkg.Tool{Name: "echo", Description: "", Schema: nil}, func(ctx context.Context, sb toolpkg.Sandbox, args map[string]any) (any, error) {
		receivedArgs = args
		return "ok", nil
	}))

	sb := &mockSandbox{name: "worktree"}
	r.RegisterSandbox("worktree", sb)

	h := NewHandler(r)
	emitter := &mockEmitter{}

	err := h.Handle(context.Background(), artifact.ToolCall{
		ID:        "call_1",
		Name:      "echo",
		Arguments: `{"sandbox": "worktree", "msg": "hello"}`,
	}, emitter)
	require.NoError(t, err)

	_, hasSandbox := receivedArgs["sandbox"]
	assert.False(t, hasSandbox)
	assert.Equal(t, "hello", receivedArgs["msg"])
}

func TestHandler_MissingSandboxName(t *testing.T) {
	r := toolpkg.NewRegistry().(toolpkg.SandboxRegistry)
	var calledWith toolpkg.Sandbox
	require.NoError(t, r.Register(toolpkg.Tool{Name: "check", Description: "", Schema: nil}, func(ctx context.Context, sb toolpkg.Sandbox, args map[string]any) (any, error) {
		calledWith = sb
		return "ok", nil
	}))

	sb := &mockSandbox{name: "worktree"}
	r.RegisterSandbox("worktree", sb)

	h := NewHandler(r)
	emitter := &mockEmitter{}

	err := h.Handle(context.Background(), artifact.ToolCall{
		ID:        "call_1",
		Name:      "check",
		Arguments: `{"sandbox": "nonexistent"}`,
	}, emitter)
	require.NoError(t, err)

	assert.Nil(t, calledWith)
}

func TestHandler_PassesNilWhenNoSandboxRegistry(t *testing.T) {
	var calledWith toolpkg.Sandbox
	r := toolpkg.NewRegistry()
	require.NoError(t, r.Register(toolpkg.Tool{Name: "check", Description: "", Schema: nil}, func(ctx context.Context, sb toolpkg.Sandbox, args map[string]any) (any, error) {
		calledWith = sb
		return "ok", nil
	}))

	h := NewHandler(r)
	emitter := &mockEmitter{}

	err := h.Handle(context.Background(), artifact.ToolCall{
		ID:        "call_1",
		Name:      "check",
		Arguments: `{}`,
	}, emitter)
	require.NoError(t, err)

	assert.Nil(t, calledWith)
}

type statusValue struct{ Foo string }

func (s statusValue) Status() map[string]string { return map[string]string{"foo": s.Foo} }

func TestHandler_StatusContributorEmitsPropertiesEvent(t *testing.T) {
	r := toolpkg.NewRegistry()
	require.NoError(t, r.Register(toolpkg.Tool{
		Name: "status",
	}, func(ctx context.Context, _ toolpkg.Sandbox, args map[string]any) (any, error) {
		return statusValue{Foo: "bar"}, nil
	}))

	h := NewHandler(r)
	emitter := &mockEmitter{}

	err := h.Handle(context.Background(), artifact.ToolCall{
		ID:        "call_1",
		Name:      "status",
		Arguments: `{}`,
	}, emitter)
	require.NoError(t, err)

	require.Len(t, emitter.events, 2)

	// First event is the TurnCompleteEvent.
	tc, ok := emitter.events[0].(loop.TurnCompleteEvent)
	require.True(t, ok)
	require.Len(t, tc.Turn.Artifacts, 1)
	tr := tc.Turn.Artifacts[0].(artifact.ToolResult)
	assert.False(t, tr.IsError)
	assert.Equal(t, `{"Foo":"bar"}`, tr.Content)

	// Second event is the PropertiesEvent from StatusContributor.
	pe, ok := emitter.events[1].(loop.PropertiesEvent)
	require.True(t, ok)
	got := make(map[string]string)
	for _, op := range pe.Operations {
		if op.Op == loop.PropertyOpSet {
			got[op.Key] = op.Value
		}
	}
	assert.Equal(t, map[string]string{"foo": "bar"}, got)
}


