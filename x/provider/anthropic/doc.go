// Package anthropic is the first-party Anthropic Messages API provider.
//
// See [New] for usage and the package overview for the rationale.
//
// # Prompt caching
//
// Prompt caching on the Anthropic Messages API is configured via
// [models.Spec.CacheControl]. When set, the wire stamps Anthropic-style
// cache_control:{type:"ephemeral",ttl:?} blocks at the system
// message, the last tool definition, and the last user/assistant
// text content part. See x/wire/anthropic for the placement rules
// and the TTL vocabulary.
//
// The first-party package does not expose a per-call
// [provider.InvokeOption] for cache control: the spec field is the
// canonical surface, mirroring how [Temperature], [ThinkingLevel],
// and [MaxOutputTokens] propagate through the spec. Per-call
// overrides are an intentional scope decision; call [models.Spec]
// directly if a specific call needs to deviate from the loop's
// default spec.
package anthropic

// This file is intentionally minimal. The first-party wrapper is a
// thin shim over the wire at github.com/andrewhowdencom/ore/x/wire/anthropic;
// see anthropic.go for the implementation.
