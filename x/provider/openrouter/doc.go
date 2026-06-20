// Package openrouter exposes the OpenRouter gateway as a first-party
// provider. OpenRouter mirrors the Anthropic Messages API at
// https://openrouter.ai/api/v1/messages and authenticates via the
// standard bearer-token header (`Authorization: Bearer <key>`),
// which the anthropic wire selects automatically when the base URL
// targets openrouter.ai.
//
// The package composes the existing anthropic wire with a
// name-resolver backed by a generated lookup table (lookup.go).
// The table is a best-effort join between canonical ore catalog
// names and OpenRouter wire names; on miss the resolver falls
// back to identity so unknown models can still be requested by
// their OpenRouter wire id verbatim.
package openrouter

// anthropicBaseURL is the OpenRouter mirror of the Anthropic
// Messages API. The wire's auth-header selection picks
// bearer-token auth when the host is not api.anthropic.com.
const anthropicBaseURL = "https://openrouter.ai/api/v1"