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

var _ Delta = TextDelta{}
var _ Delta = ReasoningDelta{}
var _ Delta = ToolCallDelta{}

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
