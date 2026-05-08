package artifact

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Compile-time interface satisfaction checks.
var _ Artifact = Text{}
var _ Artifact = ToolCall{}
var _ Artifact = Image{}
var _ Artifact = Reasoning{}

func TestArtifactKinds(t *testing.T) {
	tests := []struct {
		name string
		a    Artifact
		want string
	}{
		{"text", Text{Content: "hello"}, "text"},
		{"tool_call", ToolCall{Name: "foo", Arguments: "{}"}, "tool_call"},
		{"image", Image{URL: "http://example.com/img.png"}, "image"},
		{"reasoning", Reasoning{Content: "Let me think..."}, "reasoning"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.a.Kind())
		})
	}
}
