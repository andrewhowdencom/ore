package openaimodels_test

import (
	"testing"

	"github.com/andrewhowdencom/ore/models"
	openaimodels "github.com/andrewhowdencom/ore/x/provider/openai/models"
	"github.com/stretchr/testify/assert"
)

// TestOpenAIModels_AllHaveValidShape ensures every exported model
// Spec has the minimum viable shape: a non-empty Name (otherwise
// the provider has no model to call), a positive Window (so the
// compactor has something to compare Usage.TotalTokens against),
// and a valid ThinkingLevel (so adapters that translate the level
// to a wire format can rely on Valid() to short-circuit unknown
// values).
func TestOpenAIModels_AllHaveValidShape(t *testing.T) {
	t.Parallel()

	specs := []struct {
		name string
		spec models.Spec
	}{
		{"GPT4o", openaimodels.GPT4o},
		{"GPT4oMini", openaimodels.GPT4oMini},
		{"O1", openaimodels.O1},
		{"O1Mini", openaimodels.O1Mini},
		{"O3", openaimodels.O3},
		{"O3Mini", openaimodels.O3Mini},
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

// TestOpenAIModels_ReasoningModelsHaveThinkingOn documents the
// default thinking-level policy: reasoning models (o1, o3, …) ship
// with a non-off thinking level so callers get reasoning by default;
// non-reasoning models (gpt-4o, gpt-4o-mini) ship with thinking
// disabled. Callers can override the per-call Spec at the call site.
func TestOpenAIModels_ReasoningModelsHaveThinkingOn(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		spec models.Spec
		// want is the expected default ThinkingLevel.
		want models.ThinkingLevel
	}{
		{"GPT4o", openaimodels.GPT4o, models.ThinkingLevelOff},
		{"GPT4oMini", openaimodels.GPT4oMini, models.ThinkingLevelOff},
		{"O1", openaimodels.O1, models.ThinkingLevelMedium},
		{"O1Mini", openaimodels.O1Mini, models.ThinkingLevelMedium},
		{"O3", openaimodels.O3, models.ThinkingLevelMedium},
		{"O3Mini", openaimodels.O3Mini, models.ThinkingLevelMedium},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, tc.spec.ThinkingLevel, "%s default ThinkingLevel", tc.name)
	}
}
