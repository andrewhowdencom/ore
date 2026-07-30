package models

import "time"

// Canonical TTL values for Anthropic's ephemeral cache. These match
// the wire-format vocabulary the Anthropic API accepts
// (cache_control.ephemeral.ttl ∈ {"5m", "1h"}). Define them as
// untyped constants so they work in any time.Duration context, and
// expose them with stable names so callers don't depend on
// arithmetic that drifts across code reviews.
const (
	// CacheControlTTL5m is the 5-minute TTL. Matches Anthropic's
	// default ephemeral cache. The right choice for most
	// conversational sessions where the prefix stays in cache
	// for the duration of the conversation.
	CacheControlTTL5m = 5 * time.Minute

	// CacheControlTTL1h is the 1-hour TTL. Useful when the same
	// system prompt and tool definitions are reused across many
	// sessions within an hour (e.g. a long-running agent
	// processing many requests against a stable prefix).
	CacheControlTTL1h = time.Hour
)

// CacheControl opts a request into provider-side prompt caching. It
// is the framework-level mirror of the Anthropic wire's typed
// cache_control block (and the openai wire's behavior toggle); the
// Spec field is a *CacheControl so that nil means "no cache
// control" and any non-nil value opts the request in.
//
// The TTL is a plain time.Duration so callers can use Go's standard
// time package idiomatically (time.Minute*10, time.Hour,
// 5*time.Minute, the named constants above). The wire layer
// translates the duration to Anthropic's enumerated vocabulary
// for known values, and forwards other durations verbatim; the
// upstream API rejects values outside its vocabulary at request
// time, which is the right place to fail loudly because the
// user-facing knob is misconfigured.
//
// This type is a pure data carrier. Adapters translate it to their
// upstream's wire format at request time; the framework does not
// interpret it.
type CacheControl struct {
	// TTL is the cache-breakpoint time-to-live. Zero means "use
	// the provider default" (Anthropic: 5m); the named
	// constants CacheControlTTL5m and CacheControlTTL1h are
	// the values the Anthropic API accepts; other durations
	// are forwarded verbatim and may be rejected by the
	// upstream API at request time.
	TTL time.Duration
}
