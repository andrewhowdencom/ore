package models

// CacheControlTTL is the time-to-live the upstream provider should apply
// to a cache_control breakpoint. The vocabulary mirrors the Anthropic
// Messages API's `cache_control.ephemeral.ttl` field ("5m" / "1h"); the
// framework forwards the value verbatim on the Anthropic wire and
// ignores it where the upstream has no TTL knob (e.g. raw OpenAI).
//
// The empty string is not a valid TTL: callers that want the
// provider's default should leave the field empty rather than setting
// it to "". Use the [CacheControlTTL.Valid] method before persisting or
// surfacing user input.
type CacheControlTTL string

const (
	// CacheControlTTL5m is the 5-minute TTL. Matches Anthropic's
	// default ephemeral cache. The right choice for most
	// conversational sessions where the prefix stays in cache
	// for the duration of the conversation.
	CacheControlTTL5m CacheControlTTL = "5m"

	// CacheControlTTL1h is the 1-hour TTL. Useful when the same
	// system prompt and tool definitions are reused across many
	// sessions within an hour (e.g. a long-running agent
	// processing many requests against a stable prefix).
	CacheControlTTL1h CacheControlTTL = "1h"
)

// Valid reports whether t is one of the defined TTL constants. The
// empty string is not valid; callers should drop the field rather
// than forwarding an empty TTL when "no preference" is intended
// (the SDK tag `omitzero` drops an empty TTL on the wire).
func (t CacheControlTTL) Valid() bool {
	switch t {
	case CacheControlTTL5m, CacheControlTTL1h:
		return true
	}
	return false
}

// CacheControl opts a request into provider-side prompt caching. It
// is the framework-level mirror of the Anthropic wire's typed
// cache_control block (and the openai wire's behavior toggle); the
// Spec field is a *CacheControl so that nil means "no cache
// control" and any non-nil value opts the request in.
//
// An empty TTL is forwarded as "use the provider default" — for the
// Anthropic wire that produces the wire shape `{"type": "ephemeral"}`
// (no `ttl` field), which the API treats as 5m.
//
// This type is a pure data carrier. Adapters translate it to their
// upstream's wire format at request time; the framework does not
// interpret it.
type CacheControl struct {
	// TTL is the cache-breakpoint time-to-live. An empty value
	// means "use the provider default".
	TTL CacheControlTTL
}
