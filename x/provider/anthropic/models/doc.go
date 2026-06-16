// Package anthropicmodels exports well-known [models.Spec] values for
// Anthropic Claude models. These cover the p80 case — applications
// that need a different or fine-tuned model can construct a
// [models.Spec] directly.
//
// The catalog is vendor-local: each value is a [models.Spec] whose
// Name is the Anthropic model identifier. The Type stays shared (in
// the root models package); the Values are vendor knowledge.
//
// Recommended import pattern (alias the package to avoid clashing
// with the root [github.com/andrewhowdencom/ore/models] package):
//
//	import (
//	    "github.com/andrewhowdencom/ore/models"
//	    anthropicmodels "github.com/andrewhowdencom/ore/x/provider/anthropic/models"
//	)
//
//	opts := []provider.InvokeOption{anthropicmodels.ClaudeOpus45}
//
// The values reflect Anthropic's published model documentation at
// the time of writing; verify against the current Anthropic model
// catalog before relying on the Window and MaxOutputTokens values
// for a production deployment.
package anthropicmodels

import "github.com/andrewhowdencom/ore/models"

// ptr is a small helper for taking the address of a literal —
// pointer-typed Spec fields need an addressable value.
func ptr[T any](v T) *T { return &v }

// ClaudeOpus45 is the Anthropic Claude Opus 4.5 model: 200k
// context window, 8k output-token cap by default (Anthropic allows
// higher with extended thinking), no thinking by default.
//
// Source: https://docs.anthropic.com/en/docs/about-claude/models
var ClaudeOpus45 = models.Spec{
	Name:            "claude-opus-4-5",
	Window:          200_000,
	MaxOutputTokens: 8_192,
	Temperature:     ptr(1.0),
	ThinkingLevel:   models.ThinkingLevelOff,
}

// ClaudeSonnet45 is the Anthropic Claude Sonnet 4.5 model. Same
// window and output cap as Opus; the model is smaller and faster.
//
// Source: https://docs.anthropic.com/en/docs/about-claude/models
var ClaudeSonnet45 = models.Spec{
	Name:            "claude-sonnet-4-5",
	Window:          200_000,
	MaxOutputTokens: 8_192,
	Temperature:     ptr(1.0),
	ThinkingLevel:   models.ThinkingLevelOff,
}

// ClaudeHaiku45 is the Anthropic Claude Haiku 4.5 model. The
// smallest and fastest Claude 4.5 family member; same window and
// output cap as its siblings.
//
// Source: https://docs.anthropic.com/en/docs/about-claude/models
var ClaudeHaiku45 = models.Spec{
	Name:            "claude-haiku-4-5",
	Window:          200_000,
	MaxOutputTokens: 8_192,
	Temperature:     ptr(1.0),
	ThinkingLevel:   models.ThinkingLevelOff,
}
