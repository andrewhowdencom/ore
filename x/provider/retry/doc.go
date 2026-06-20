// Package retry implements a provider.Provider decorator that
// transparently retries transient upstream failures (HTTP 5xx, 429 with
// Retry-After) on long-running LLM sessions.
//
// # Design
//
// The decorator is a thin pass-through around provider.Invoke. The
// default classifier recognises 5xx and 429 responses via the
// HTTPError interface, which is satisfied by SDK-specific error
// wrappers. Adapters implement HTTPError to participate in retry; the
// retry package itself does not import any vendor SDK.
//
// # Streaming rule
//
// Once an attempt has emitted any artifact, the decorator will not
// retry. A retried attempt would re-emit a partial response, which
// both confuses subscribers and risks double-billing on token-based
// providers. The rule applies per attempt (emission state resets
// between attempts).
//
// # Tracing
//
// WithTracer enables a retry.invoke span that spans all attempts of a
// single Invoke call. Each attempt transition is recorded as a
// "retry.attempt" event; the final attempt count is attached as a
// "retry.attempts" attribute on the span.
package retry
