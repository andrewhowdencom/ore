package anthropicmodels_test

import (
	"testing"

	"github.com/andrewhowdencom/ore/models"
	anthropicmodels "github.com/andrewhowdencom/ore/x/provider/anthropic/models"
	"github.com/stretchr/testify/assert"
)

// TestAnthropicModels_AllHaveValidShape ensures every exported
// model Spec has the minimum viable shape: a non-empty Name
// (otherwise the provider has no model to call), a positive Window
// (so the compactor has something to compare Usage.TotalTokens
// against), and a valid ThinkingLevel (so adapters that translate
// the level to a wire format can rely on Valid() to short-circuit
// unknown values).
func TestAnthropicModels_AllHaveValidShape(t *testing.T) {
	t.Parallel()

	specs := []struct {
		name string
		spec models.Spec
	}{
		{"ClaudeOpus45", anthropicmodels.ClaudeOpus45},
		{"ClaudeSonnet45", anthropicmodels.ClaudeSonnet45},
		{"ClaudeHaiku45", anthropicmodels.ClaudeHaiku45},
	}

	for _, tc := range specs {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.NotEmpty(t, tc.spec.Name, "%s.Name is empty", tc.name)
			assert.Positive(t, tc.spec.Window, "%s.Window is not positive", tc.name)
			assert.True(t, tc.spec.ThinkingLevel.Valid(), "%s.ThinkingLevel is invalid: %q", tc.name, tc.spec.ThinkingLevel)
		})
	}
}

// TestAnthropicModels_HaveAnthropicNaming documents the convention
// that all Anthropic model specs use the unversioned name (e.g.
// "claude-opus-4-5") rather than a dated snapshot ("claude-opus-4-5-20250929").
// Callers that need a specific snapshot use ad-hoc Spec construction.
func TestAnthropicModels_HaveAnthropicNaming(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		spec models.Spec
	}{
		{"ClaudeOpus45", anthropicmodels.ClaudeOpus45},
		{"ClaudeSonnet45", anthropicmodels.ClaudeSonnet45},
		{"ClaudeHaiku45", anthropicmodels.ClaudeHaiku45},
	}
	for _, tc := range cases {
		assert.Contains(t, tc.spec.Name, "claude-", "%s.Name should be an Anthropic Claude model identifier", tc.name)
	}
}
