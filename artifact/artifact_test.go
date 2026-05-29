package artifact

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Compile-time interface satisfaction checks.
var _ Artifact = Text{}
var _ Artifact = ToolCall{}
var _ Artifact = ToolResult{}
var _ Artifact = Usage{}
var _ Artifact = Image{}
var _ Artifact = Reasoning{}

var _ LLMRenderer = (*mockLLMRenderer)(nil)
var _ MarkdownRenderer = (*mockMarkdownRenderer)(nil)

type mockLLMRenderer struct{}

func (m *mockLLMRenderer) MarshalLLM() string { return "llm" }

type mockMarkdownRenderer struct{}

func (m *mockMarkdownRenderer) MarshalMarkdown() string { return "markdown" }

var _ Delta = TextDelta{}
var _ Delta = ReasoningDelta{}
var _ Delta = ToolCallDelta{}

var _ Accumulable = TextDelta{}
var _ Accumulable = ReasoningDelta{}
var _ Accumulable = ToolCallDelta{}

func TestDeltaArtifacts(t *testing.T) {
	// Delta types should satisfy the Delta interface.
	assert.Implements(t, (*Delta)(nil), TextDelta{})
	assert.Implements(t, (*Delta)(nil), ReasoningDelta{})
	assert.Implements(t, (*Delta)(nil), ToolCallDelta{})

	// Non-delta types should NOT satisfy the Delta interface.
	assert.False(t, isDelta(Text{}))
	assert.False(t, isDelta(ToolCall{}))
	assert.False(t, isDelta(ToolResult{}))
	assert.False(t, isDelta(Usage{}))
	assert.False(t, isDelta(Image{}))
	assert.False(t, isDelta(Reasoning{}))
}

func isDelta(a Artifact) bool {
	_, ok := a.(Delta)
	return ok
}

func TestArtifactKinds(t *testing.T) {
	tests := []struct {
		name string
		a    Artifact
		want string
	}{
		{"text", Text{Content: "hello"}, "text"},
		{"tool_call", ToolCall{Name: "foo", Arguments: "{}"}, "tool_call"},
		{"image", Image{URL: "http://example.com/img.png"}, "image"},
		{"tool_call", ToolCall{Name: "foo", Arguments: "{}"}, "tool_call"},
		{"tool_result", ToolResult{ToolCallID: "call_1", Content: "ok"}, "tool_result"},
		{"usage", Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}, "usage"},
		{"image", Image{URL: "http://example.com/img.png"}, "image"},
		{"reasoning", Reasoning{Content: "Let me think..."}, "reasoning"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.a.Kind())
		})
	}
}

func TestAccumulableInterface(t *testing.T) {
	assert.Implements(t, (*Accumulable)(nil), TextDelta{})
	assert.Implements(t, (*Accumulable)(nil), ReasoningDelta{})
	assert.Implements(t, (*Accumulable)(nil), ToolCallDelta{})
}

func TestToolResult_ValueField(t *testing.T) {
	tr := ToolResult{ToolCallID: "call_1", Content: "ok", Value: 42, IsError: false}
	assert.Equal(t, "call_1", tr.ToolCallID)
	assert.Equal(t, "ok", tr.Content)
	assert.Equal(t, 42, tr.Value)
	assert.False(t, tr.IsError)
}

func TestAccumulable_MergeInto_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		delta    Accumulable
		acc      Artifact
		expected Artifact
	}{
		{
			name:     "TextDelta seeds new Text when acc is nil",
			delta:    TextDelta{Content: "hello"},
			acc:      nil,
			expected: Text{Content: "hello"},
		},
		{
			name:     "TextDelta merges into existing Text",
			delta:    TextDelta{Content: " world"},
			acc:      Text{Content: "hello"},
			expected: Text{Content: "hello world"},
		},
		{
			name:     "ReasoningDelta seeds new Reasoning when acc is nil",
			delta:    ReasoningDelta{Content: "think"},
			acc:      nil,
			expected: Reasoning{Content: "think"},
		},
		{
			name:     "ReasoningDelta merges into existing Reasoning",
			delta:    ReasoningDelta{Content: " deeply"},
			acc:      Reasoning{Content: "think"},
			expected: Reasoning{Content: "think deeply"},
		},
		{
			name:     "ToolCallDelta seeds new ToolCall when acc is nil",
			delta:    ToolCallDelta{Index: 0, ID: "call_1", Name: "search", Arguments: "q"},
			acc:      nil,
			expected: ToolCall{ID: "call_1", Name: "search", Arguments: "q"},
		},
		{
			name:     "ToolCallDelta concatenates Name and Arguments",
			delta:    ToolCallDelta{Index: 0, ID: "", Name: "calc", Arguments: "1+"},
			acc:      ToolCall{ID: "call_1", Name: "search", Arguments: "q"},
			expected: ToolCall{ID: "call_1", Name: "searchcalc", Arguments: "q1+"},
		},
		{
			name:     "ToolCallDelta latest-wins overwrites ID",
			delta:    ToolCallDelta{Index: 0, ID: "call_2", Name: "", Arguments: ""},
			acc:      ToolCall{ID: "call_1", Name: "search", Arguments: "q"},
			expected: ToolCall{ID: "call_2", Name: "search", Arguments: "q"},
		},
		{
			name:     "ToolCallDelta empty ID preserves existing ID",
			delta:    ToolCallDelta{Index: 0, ID: "", Name: "calc", Arguments: "1"},
			acc:      ToolCall{ID: "call_1", Name: "search", Arguments: "q"},
			expected: ToolCall{ID: "call_1", Name: "searchcalc", Arguments: "q1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.delta.MergeInto(tt.acc)
			assert.Equal(t, tt.expected, result)
		})
	}
}
