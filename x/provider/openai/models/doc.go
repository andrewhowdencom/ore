// Package openaimodels exports well-known [models.Spec] values for
// OpenAI-compatible chat completion models. These cover the p80 case
// — applications that need a different or fine-tuned model can
// construct a [models.Spec] directly.
//
// The catalog is vendor-local: each value is a [models.Spec] whose
// Name is the OpenAI model identifier. The Type stays shared (in the
// root models package); the Values are vendor knowledge.
//
// Recommended import pattern (alias the package to avoid clashing
// with the root [github.com/andrewhowdencom/ore/models] package):
//
//	import (
//	    "github.com/andrewhowdencom/ore/models"
//	    openaimodels "github.com/andrewhowdencom/ore/x/provider/openai/models"
//	)
//
//	opts := []provider.InvokeOption{openaimodels.GPT4o}
//
// The values reflect OpenAI's published model documentation at the
// time of writing; verify against the current OpenAI model catalog
// before relying on the Window and MaxOutputTokens values for a
// production deployment.
package openaimodels

import "github.com/andrewhowdencom/ore/models"

// ptr is a small helper for taking the address of a literal —
// pointer-typed Spec fields need an addressable value.
func ptr[T any](v T) *T { return &v }

// GPT4o is the OpenAI gpt-4o model: 128k context window, 16k
// output-token cap, no extended thinking by default.
//
// Source: https://platform.openai.com/docs/models/gpt-4o
var GPT4o = models.Spec{
	Name:            "gpt-4o",
	Window:          128_000,
	MaxOutputTokens: 16_384,
	Temperature:     ptr(1.0),
	ThinkingLevel:   models.ThinkingLevelOff,
}

// GPT4oMini is the smaller, faster gpt-4o-mini. Same window and
// output cap as GPT4o, but a smaller and cheaper model.
//
// Source: https://platform.openai.com/docs/models/gpt-4o-mini
var GPT4oMini = models.Spec{
	Name:            "gpt-4o-mini",
	Window:          128_000,
	MaxOutputTokens: 16_384,
	Temperature:     ptr(1.0),
	ThinkingLevel:   models.ThinkingLevelOff,
}

// O1 is the o1 reasoning model: 200k context window, 100k output
// cap, medium reasoning effort by default. The high ThinkingLevel
// default reflects that o1 is a reasoning model and the user
// typically wants it to think.
//
// Source: https://platform.openai.com/docs/models/o1
var O1 = models.Spec{
	Name:            "o1",
	Window:          200_000,
	MaxOutputTokens: 100_000,
	ThinkingLevel:   models.ThinkingLevelMedium,
}

// O1Mini is the smaller o1-mini reasoning model. Same window as
// o1 but with a 65k output cap; the default thinking level is
// still medium.
//
// Source: https://platform.openai.com/docs/models/o1-mini
var O1Mini = models.Spec{
	Name:            "o1-mini",
	Window:          128_000,
	MaxOutputTokens: 65_536,
	ThinkingLevel:   models.ThinkingLevelMedium,
}

// O3 is the o3 reasoning model. 200k context window, 100k output
// cap, medium thinking by default.
//
// Source: https://platform.openai.com/docs/models/o3
var O3 = models.Spec{
	Name:            "o3",
	Window:          200_000,
	MaxOutputTokens: 100_000,
	ThinkingLevel:   models.ThinkingLevelMedium,
}

// O3Mini is the smaller o3-mini reasoning model. Same window and
// output cap as o3, but cheaper per token.
//
// Source: https://platform.openai.com/docs/models/o3-mini
var O3Mini = models.Spec{
	Name:            "o3-mini",
	Window:          200_000,
	MaxOutputTokens: 100_000,
	ThinkingLevel:   models.ThinkingLevelMedium,
}
