// Package openai provides the first-party OpenAI provider adapter.
//
// It is the recommended import path for application code that wants to
// call the OpenAI Chat Completions API (and the OpenAI-compatible
// surface exposed by the wire). The package composes the wire
// implementation at github.com/andrewhowdencom/ore/x/wire/openai and
// applies first-party defaults (currently identity — canonical spec
// names are forwarded verbatim to the upstream API).
//
// Application code imports only this package; the wire is a transitive
// dependency that does not leak into the public API beyond the
// [Option] type alias. Future versions may add vendor-specific defaults
// (catalog, base URL, identity resolver) here without breaking call
// sites — the first-party wrapper exists precisely so that a single
// package can absorb such changes.
package openai

import (
	"github.com/andrewhowdencom/ore/provider"
	openaiwire "github.com/andrewhowdencom/ore/x/wire/openai"
)

// Option configures a first-party OpenAI provider. It is an alias of
// [openaiwire.Option] so callers can write `openai.WithAPIKey(...)`
// or import the wire's options directly — both forms are accepted by
// [New].
type Option = openaiwire.Option

// New constructs a first-party OpenAI provider. The first-party
// wrapper currently composes the wire implementation with identity
// resolution (canonical spec names forwarded verbatim). Vendor-specific
// defaults — catalog lookup, a default base URL, a custom name
// resolver — are intended to live here in future versions without
// breaking call sites.
//
// The returned value implements [provider.Provider] but is the wire's
// concrete *Provider type; callers should depend on the interface.
func New(opts ...Option) (provider.Provider, error) {
	return openaiwire.New(opts...)
}