package tool

import (
	"context"
	"strings"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	toolpkg "github.com/andrewhowdencom/ore/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// bigString returns a string of n bytes. Used to fabricate tool
// results that exceed the framework's default 50 KB cap.
func bigString(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat("a", n)
}

func TestHandler_AppliesDefaultTruncation_10MBString(t *testing.T) {
	t.Parallel()

	r := toolpkg.NewRegistry()
	require.NoError(t, r.Register(toolpkg.Tool{
		Name: "big",
	}, func(ctx context.Context, _ toolpkg.Sandbox, args map[string]any) (any, error) {
		return bigString(10_000_000), nil
	}))

	h := NewHandler(r)
	emitter := &mockEmitter{}

	err := h.Handle(context.Background(), artifact.ToolCall{
		ID:        "call_1",
		Name:      "big",
		Arguments: `{}`,
	}, emitter)
	require.NoError(t, err)

	require.Len(t, emitter.events, 1)
	tc := emitter.events[0].(loop.TurnCompleteEvent)
	require.Len(t, tc.Turn.Artifacts, 1)
	tr := tc.Turn.Artifacts[0].(artifact.ToolResult)

	// Content is the truncated string + a recovery hint + the
	// "lines shown" notice. The pre-hint length must be ≤ 50 KB.
	// We can't assert on total length because the recovery hint
	// and notice add a small fixed amount, so check the
	// Truncation descriptor instead.
	require.NotNil(t, tr.Truncation, "Truncation should be non-nil for a 10 MB result")
	// json.Marshal wraps the string in quotes, adding 2 bytes.
	assert.GreaterOrEqual(t, tr.Truncation.OriginalBytes, 10_000_000)
	assert.LessOrEqual(t, tr.Truncation.ShownBytes, toolpkg.FrameworkDefaultMaxBytes)
	assert.True(t, tr.Truncation.Truncated())
	assert.Equal(t, "tail", tr.Truncation.Style)
}

func TestHandler_RespectsLLMRenderer(t *testing.T) {
	t.Parallel()

	r := toolpkg.NewRegistry()
	require.NoError(t, r.Register(toolpkg.Tool{
		Name: "big",
	}, func(ctx context.Context, _ toolpkg.Sandbox, args map[string]any) (any, error) {
		return bigStringRenderer{content: bigString(10_000_000)}, nil
	}))

	h := NewHandler(r)
	emitter := &mockEmitter{}

	err := h.Handle(context.Background(), artifact.ToolCall{
		ID:        "call_1",
		Name:      "big",
		Arguments: `{}`,
	}, emitter)
	require.NoError(t, err)

	tc := emitter.events[0].(loop.TurnCompleteEvent)
	tr := tc.Turn.Artifacts[0].(artifact.ToolResult)

	// LLMRenderer content is preserved verbatim.
	assert.Equal(t, bigString(10_000_000), tr.Content)
	assert.Nil(t, tr.Truncation, "LLMRenderer opts out of truncation")
}

// bigStringRenderer implements artifact.LLMRenderer for tests.
type bigStringRenderer struct{ content string }

func (b bigStringRenderer) MarshalLLM() string { return b.content }

func TestHandler_AppliesToolSpecificFormat(t *testing.T) {
	t.Parallel()

	r := toolpkg.NewRegistry()
	require.NoError(t, r.Register(toolpkg.Tool{
		Name: "custom",
		Format: toolpkg.Format{
			Truncate: toolpkg.TruncateConfig{MaxBytes: 100},
		},
	}, func(ctx context.Context, _ toolpkg.Sandbox, args map[string]any) (any, error) {
		return bigString(10_000), nil
	}))

	h := NewHandler(r)
	emitter := &mockEmitter{}

	err := h.Handle(context.Background(), artifact.ToolCall{
		ID:        "call_1",
		Name:      "custom",
		Arguments: `{}`,
	}, emitter)
	require.NoError(t, err)

	tc := emitter.events[0].(loop.TurnCompleteEvent)
	tr := tc.Turn.Artifacts[0].(artifact.ToolResult)

	require.NotNil(t, tr.Truncation)
	assert.LessOrEqual(t, tr.Truncation.ShownBytes, 100)
}

func TestHandler_AppliesRecoveryHint(t *testing.T) {
	t.Parallel()

	r := toolpkg.NewRegistry()
	require.NoError(t, r.Register(toolpkg.Tool{
		Name: "read",
		Format: toolpkg.Format{
			Truncate:     toolpkg.TruncateConfig{MaxBytes: 10},
			Style:        toolpkg.StyleHead,
			RecoveryHint: "Use offset={next_offset} to continue reading.",
		},
	}, func(ctx context.Context, _ toolpkg.Sandbox, args map[string]any) (any, error) {
		// 20 lines of 10 bytes each.
		var sb strings.Builder
		for i := 0; i < 20; i++ {
			sb.WriteString("line....\n")
		}
		return sb.String(), nil
	}))

	h := NewHandler(r)
	emitter := &mockEmitter{}

	err := h.Handle(context.Background(), artifact.ToolCall{
		ID:        "call_1",
		Name:      "read",
		Arguments: `{}`,
	}, emitter)
	require.NoError(t, err)

	tc := emitter.events[0].(loop.TurnCompleteEvent)
	tr := tc.Turn.Artifacts[0].(artifact.ToolResult)

	assert.Contains(t, tr.Content, "Use offset=")
	assert.Contains(t, tr.Content, "to continue reading.")
	assert.Contains(t, tr.Content, "lines shown of")
}

func TestHandler_TruncatesNamespacedResults(t *testing.T) {
	t.Parallel()

	remote := &bigRemoteSource{
		name: "remote",
		tools: []toolpkg.Tool{
			// No Format declared: framework default applies.
			{Name: "big", Description: "Big", Schema: map[string]any{"type": "object"}},
		},
	}

	r := toolpkg.NewRegistry(toolpkg.WithMCPServer(remote))
	h := NewHandler(r)
	emitter := &mockEmitter{}

	err := h.Handle(context.Background(), artifact.ToolCall{
		ID:        "call_1",
		Name:      "remote/big",
		Arguments: `{}`,
	}, emitter)
	require.NoError(t, err)

	tc := emitter.events[0].(loop.TurnCompleteEvent)
	tr := tc.Turn.Artifacts[0].(artifact.ToolResult)

	require.NotNil(t, tr.Truncation)
	assert.LessOrEqual(t, tr.Truncation.ShownBytes, toolpkg.FrameworkDefaultMaxBytes)
}

func TestHandler_NoTruncationUnderCap(t *testing.T) {
	t.Parallel()

	r := toolpkg.NewRegistry()
	require.NoError(t, r.Register(toolpkg.Tool{
		Name: "small",
	}, func(ctx context.Context, _ toolpkg.Sandbox, args map[string]any) (any, error) {
		return "small result", nil
	}))

	h := NewHandler(r)
	emitter := &mockEmitter{}

	err := h.Handle(context.Background(), artifact.ToolCall{
		ID:        "call_1",
		Name:      "small",
		Arguments: `{}`,
	}, emitter)
	require.NoError(t, err)

	tc := emitter.events[0].(loop.TurnCompleteEvent)
	tr := tc.Turn.Artifacts[0].(artifact.ToolResult)

	assert.Equal(t, `"small result"`, tr.Content)
	assert.Nil(t, tr.Truncation, "small result should not be truncated")
}

func TestHandler_TruncationMetadataPreservedOnError(t *testing.T) {
	t.Parallel()

	r := toolpkg.NewRegistry()
	require.NoError(t, r.Register(toolpkg.Tool{
		Name: "partial",
	}, func(ctx context.Context, _ toolpkg.Sandbox, args map[string]any) (any, error) {
		return bigString(1_000_000), assertErr("partial failure")
	}))

	h := NewHandler(r)
	emitter := &mockEmitter{}

	err := h.Handle(context.Background(), artifact.ToolCall{
		ID:        "call_1",
		Name:      "partial",
		Arguments: `{}`,
	}, emitter)
	require.NoError(t, err)

	require.Len(t, emitter.events, 1)
	tc := emitter.events[0].(loop.TurnCompleteEvent)
	tr := tc.Turn.Artifacts[0].(artifact.ToolResult)

	assert.True(t, tr.IsError)
	require.NotNil(t, tr.Truncation, "partial result should be truncated even on error")
	assert.LessOrEqual(t, tr.Truncation.ShownBytes, toolpkg.FrameworkDefaultMaxBytes)
}

// assertErr is a tiny helper that returns a new error each time.
type assertErr string

func (e assertErr) Error() string { return string(e) }

// bigRemoteSource returns a 10 MB string from Call to exercise
// truncation in the namespaced path.
type bigRemoteSource struct {
	name  string
	tools []toolpkg.Tool
}

func (b *bigRemoteSource) Name() string { return b.name }
func (b *bigRemoteSource) Tools() []toolpkg.Tool {
	out := make([]toolpkg.Tool, len(b.tools))
	copy(out, b.tools)
	return out
}
func (b *bigRemoteSource) Call(ctx context.Context, name string, args map[string]any) (any, error) {
	return bigString(10_000_000), nil
}

func TestHandler_TraceAttributesOnTruncation(t *testing.T) {
	t.Parallel()

	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	r := toolpkg.NewRegistry()
	require.NoError(t, r.Register(toolpkg.Tool{Name: "big"}, func(ctx context.Context, _ toolpkg.Sandbox, args map[string]any) (any, error) {
		return bigString(10_000_000), nil
	}))

	h := NewHandler(r, WithTracer(tp.Tracer("test")))
	emitter := &mockEmitter{}

	err := h.Handle(context.Background(), artifact.ToolCall{
		ID:        "call_1",
		Name:      "big",
		Arguments: `{}`,
	}, emitter)
	require.NoError(t, err)

	spans := sr.Ended()
	require.NotEmpty(t, spans, "expected at least one span")
	// The tool.execute span should have truncation attributes.
	toolSpan := spans[0]
	attrs := toolSpan.Attributes()

	hasTruncated := false
	var originalBytes, shownBytes attribute.Value
	for _, a := range attrs {
		switch string(a.Key) {
		case "tool.truncated":
			if a.Value.AsBool() {
				hasTruncated = true
			}
		case "tool.truncation.original_bytes":
			originalBytes = a.Value
		case "tool.truncation.shown_bytes":
			shownBytes = a.Value
		}
	}
	assert.True(t, hasTruncated, "tool.truncated should be true")
	assert.Equal(t, int64(10_000_002), originalBytes.AsInt64(), "original_bytes should reflect the 10 MB string + JSON quotes")
	assert.LessOrEqual(t, shownBytes.AsInt64(), int64(toolpkg.FrameworkDefaultMaxBytes))
}
