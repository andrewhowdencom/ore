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
// WithThinkingLevel(level) enables Anthropic's `thinking` config on the
// outgoing request when the level is not off. The level is translated
// to a thinking.budget_tokens value at request time, expressed as a
// percentage of max_tokens:
//
//	minimal -> 2%   (floored to 1024)
//	low     -> 8%
//	medium  -> 25%
//	high    -> 50%
//	max     -> 80%  (capped at max_tokens - 1024)
//
// The floor of 1024 is Anthropic's hard minimum; the ceiling of
// (max_tokens - 1024) guarantees the visible response has at least
// 1024 tokens to work with. provider.ThinkingLevelOff (or the empty
// level) disables extended thinking entirely; the SDK receives no
// `thinking` field and the upstream model falls back to its
// non-thinking default behavior.
//
// The provider streams `thinking_delta` events back as ReasoningDelta
// artifacts and surfaces each completed `thinking` block's `signature`
// as a ReasoningSignature{Provider: "anthropic", SubKind: "signature"}
// at the close of the block, so the next turn's serializer can merge
// it into the replayed `thinking` block. A `redacted_thinking` block
// produces a ReasoningSignature{Provider: "anthropic", SubKind:
// "redacted"} so the opaque encrypted reasoning can be carried forward.
//
// # Cache metrics
//
// When the host reports cache statistics in the streaming `usage` block,
// the provider maps them to the artifact's CacheReadTokens and
// CacheWriteTokens fields. The new ThinkingTokens field is populated
// from usage.output_tokens_details.thinking_tokens when present.
//
// # Prompt caching
//
// The provider honors models.Spec.CacheControl on the request side.
// When non-nil, the wire stamps Anthropic-style
// cache_control:{type:"ephemeral",ttl:?} blocks at three locations
// (the system message, the last tool definition, and the last
// user/assistant text content part), mirroring the placement logic
// the openai wire uses for its applyCacheControl. Missing targets
// are skipped silently.
//
// The TTL flows from models.CacheControlTTL through to the SDK's
// CacheControlEphemeralTTL via translateCacheControlTTL. An empty
// TTL drops the field on the wire and the upstream uses its
// default 5m; unknown TTLs are forwarded verbatim so the upstream
// API surfaces a validation error at request time.
//
// The wire does not cache responses locally: the framework observes
// Anthropic's server-side cache hits via the read-side metrics
// above, but the next Invoke() always goes out over the wire.
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
