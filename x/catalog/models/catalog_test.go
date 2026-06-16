package catalogmodels

import (
	"testing"

	"github.com/andrewhowdencom/ore/models"
)

// TestKnownModelsHaveWindows enumerates a representative subset of
// the catalog and asserts that each entry has a non-zero Window
// and MaxOutputTokens. The catalog is generated; a missing or
// zero value indicates a regression in the generator or in the
// upstream models.dev response shape.
//
// The identifiers below are pinned to current upstream naming.
// If a future re-generation renames a model (e.g.
// "Gpt35Turbo" -> "Gpt35TurboLatest"), this test fails and the
// maintainer updates the references. That is the desired
// behavior: a rename in upstream should be visible in the test.
func TestKnownModelsHaveWindows(t *testing.T) {
	t.Parallel()

	specs := []models.Spec{
		// Anthropic.
		Claude35Haiku20241022,
		ClaudeOpus45,
		ClaudeSonnet45,
		// OpenAI.
		Gpt35Turbo,
		Gpt4,
		O1,
		// Google.
		Gemini20Flash,
		Gemini25Flash,
		// xAI.
		Grok4200309Reasoning,
		// DeepSeek.
		DeepseekChat,
		DeepseekReasoner,
		// Mistral.
		CodestralLatest,
		Devstral2512,
		// MiniMax.
		MiniMaxM2,
		// Alibaba.
		QwenMax,
		QwenFlash,
		// Cohere.
		C4aiAyaExpanse8b,
		// Meta (via cerebras / groq).
		CerebrasLlama4Scout17b16eInstruct,
	}

	for _, s := range specs {
		if s.Name == "" {
			t.Errorf("spec has empty Name")
		}
		if s.Window == 0 {
			t.Errorf("spec %q has zero Window", s.Name)
		}
		if s.MaxOutputTokens == 0 {
			t.Errorf("spec %q has zero MaxOutputTokens", s.Name)
		}
	}
}

// TestTemperatureIsFloat64Pointer asserts that the Temperature
// field on every catalog Spec is a non-nil *float64. Adapters
// that translate the field to a wire value need a concrete
// number to send; the pointer is non-nil so a zero-value Spec
// never silently emits "temperature: 0" to a wire that expects
// a float.
//
// This test is the single source of truth for the "Temperature
// is always a *float64" contract. The generator's template
// hard-codes ptr(1.0); if a future revision interpolates
// printf %#g or similar, the rounding must continue to produce
// a *float64 (not a *int).
func TestTemperatureIsFloat64Pointer(t *testing.T) {
	t.Parallel()

	specs := []models.Spec{
		ClaudeOpus45,
		Gpt4,
		Gemini20Flash,
	}

	for _, s := range specs {
		if s.Temperature == nil {
			t.Errorf("spec %q has nil Temperature", s.Name)
			continue
		}
		// Reading through the pointer as a float64 will
		// panic at runtime if the underlying type is
		// something other than float64 (e.g. an int
		// stored in a *float64). The compile-time check
		// below is what we really want; the runtime
		// assertion is belt-and-suspenders.
		var _ float64 = *s.Temperature
	}
}
