package minimax

import (
	"github.com/andrewhowdencom/ore/provider"
	anthropicwire "github.com/andrewhowdencom/ore/x/wire/anthropic"
	openaiwire "github.com/andrewhowdencom/ore/x/wire/openai"
)

// identityResolver returns its input unchanged. It is exported so
// callers can compose their own resolvers on top of the identity
// default (e.g. `func(canonical string) string { if mapped, ok := table[canonical]; ok { return mapped }; return identity(canonical) }`).
func identityResolver(canonical string) string {
	return canonical
}

// NewAnthropic constructs a provider speaking the Anthropic Messages
// API on api.minimax.io. The returned provider applies an identity
// name resolver so that canonical spec names from the ore catalog
// are forwarded verbatim to the upstream host.
//
// The wire's [github.com/andrewhowdencom/ore/x/wire/anthropic.WithNameResolver]
// option remains the escape hatch when the upstream adopts aliases.
func NewAnthropic(apiKey string) (provider.Provider, error) {
	return anthropicwire.New(
		anthropicwire.WithAPIKey(apiKey),
		anthropicwire.WithBaseURL(anthropicBaseURL),
		anthropicwire.WithNameResolver(identityResolver),
	)
}

// NewOpenAI constructs a provider speaking the OpenAI Chat Completions
// API on api.minimax.io. The returned provider applies an identity
// name resolver so that canonical spec names from the ore catalog
// are forwarded verbatim to the upstream host.
//
// The wire's [github.com/andrewhowdencom/ore/x/wire/openai.WithNameResolver]
// option remains the escape hatch when the upstream adopts aliases.
func NewOpenAI(apiKey string) (provider.Provider, error) {
	return openaiwire.New(
		openaiwire.WithAPIKey(apiKey),
		openaiwire.WithBaseURL(openaiBaseURL),
		openaiwire.WithNameResolver(identityResolver),
	)
}