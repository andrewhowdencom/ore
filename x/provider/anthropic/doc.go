// Package anthropic implements a provider adapter for the Anthropic Messages
// API and the OpenRouter /api/v1/messages mirror.
//
// It uses the official anthropic-sdk-go SDK and supports custom base URLs for
// local proxies or alternative endpoints.
//
// # Extended thinking
//
// The provider surfaces one per-invocation option for extended-thinking
// configuration, plus reasoning / signature replay on both the read and
// write sides.
//
// WithThinkingBudget(tokens) enables Anthropic's `thinking` config on the
// outgoing request when the budget is non-zero. The provider streams
// `thinking_delta` events back as ReasoningDelta artifacts and surfaces
// each completed `thinking` block's `signature` as a
// ReasoningSignature{Provider: "anthropic", SubKind: "signature"} at the
// close of the block, so the next turn's serializer can merge it into
// the replayed `thinking` block. A `redacted_thinking` block produces a
// ReasoningSignature{Provider: "anthropic", SubKind: "redacted"} so the
// opaque encrypted reasoning can be carried forward.
//
// # Cache metrics
//
// When the host reports cache statistics in the streaming `usage` block,
// the provider maps them to the artifact's CacheReadTokens and
// CacheWriteTokens fields. The new ThinkingTokens field is populated
// from usage.output_tokens_details.thinking_tokens when present.
//
// # Host-aware auth
//
// WithAPIKey(key) sets the right header depending on the configured
// base URL: on Anthropic native the key is sent as `x-api-key`, on
// OpenRouter's /api/v1/messages mirror the key is sent as
// `Authorization: Bearer <key>`. The auth header is applied at
// construction time, not per invocation, so it cannot drift between
// turns of the same provider.
package anthropic
