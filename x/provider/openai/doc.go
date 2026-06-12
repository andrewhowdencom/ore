// Package openai implements a provider adapter for OpenAI-compatible chat
// completions APIs.
//
// It uses the official OpenAI Go SDK and supports custom base URLs for
// local proxies or alternative endpoints.
//
// # Cache control
//
// The provider surfaces two per-invocation options for prompt-cache
// affinity, plus usage reading on the response side. Their behavior
// varies by host because OpenAI native, OpenRouter, and
// Anthropic-via-OpenRouter each model prompt caching differently.
//
// WithSessionID(string) sets the request's prompt_cache_key. WithCacheControl()
// emits Anthropic-style cache_control:{type:ephemeral} blocks on the
// system message, the last tool definition, and the last user/assistant
// text content part. The provider always emits a single artifact.Usage
// per stream; the new CacheReadTokens and CacheWriteTokens fields are
// populated when the host reports them and are zero (and therefore
// omitted from the JSON payload) otherwise.
//
//   - OpenAI native: WithSessionID is honored (prompt_cache_key enables
//     prefix-routing affinity). WithCacheControl is a no-op: the host
//     ignores cache_control blocks. CacheReadTokens is populated from
//     usage.prompt_tokens_details.cached_tokens on a cache hit.
//   - OpenRouter: WithSessionID is honored. WithCacheControl is honored
//     and produces full Anthropic-style cache discounts on hosts that
//     proxy to Anthropic, or partial discounts on the few OpenRouter
//     models that surface cache metrics. CacheReadTokens and
//     CacheWriteTokens are populated from the top-level
//     cache_read_input_tokens / cache_creation_input_tokens fields on
//     Anthropic-via-OpenRouter.
//   - Anthropic-via-OpenRouter: identical to OpenRouter above.
//   - Other openai-compatible hosts: best-effort. The provider uses
//     SetExtraFields to inject cache_control and reasoning blocks, so
//     unknown fields are dropped by the host. No regression for users
//     that do not call WithCacheControl.
package openai
