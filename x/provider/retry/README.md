# Provider Retry Decorator

A `provider.Provider` decorator that transparently retries transient upstream
failures (HTTP 5xx, 429 with `Retry-After`) on long-running LLM sessions.

## Quick Start

```go
import "github.com/andrewhowdencom/ore/x/provider/retry"

// Wrap any provider.Provider with the default retry policy.
decorated := retry.New(inner)

// Or, with custom options:
decorated := retry.New(inner,
    retry.WithMaxAttempts(5),
    retry.WithBaseDelay(200*time.Millisecond),
    retry.WithMaxDelay(5*time.Second),
    retry.WithHonorRetryAfter(true),
    retry.WithClassifier(myClassifier),
    retry.WithTracer(tracer),
)
```

The decorated provider is a drop-in replacement for the inner one — same
`Invoke(ctx, state, spec, ch, opts...)` signature, same streaming
contract.

## Default Behavior

| Setting | Default |
|---------|---------|
| `MaxAttempts` | `3` (initial + 2 retries) |
| `BaseDelay` | `500ms` |
| `MaxDelay` | `30s` |
| `HonorRetryAfter` | `true` |
| Classifier | `DefaultClassifier` (5xx and 429 only) |

The default classifier recognizes:

- **HTTP 5xx** (500, 502, 503, 504, …) — retry, honor `Retry-After` if present.
- **HTTP 429** — retry, honor `Retry-After` if present.
- All other errors (4xx, transport errors, context cancellation) — fail
  immediately.

The backoff is exponential with equal jitter (`RandomizationFactor = 0.5`):
the actual delay for attempt N is in `[interval*0.5, interval*1.5]` where
`interval = base * 2^(N-1)`, capped at `MaxDelay`.

## Streaming Rule

Once any artifact has been emitted on the current attempt, the decorator
**will not retry** — even if the next attempt is classified as retriable.

This is a hard backstop. A retried attempt would re-emit a partial
response, which both confuses subscribers and risks double-billing on
token-based providers. The rule is per attempt (emission state resets
between attempts).

The classifier still runs on the failure, but its verdict is ignored once
emission has started: the decorator returns the error verbatim so the
caller sees the failure that produced the partial response.

## HTTPError Interface

The retry package does not import any vendor SDK. Adapters wrap their SDK
errors in types that implement this minimal interface:

```go
type HTTPError interface {
    error
    StatusCode() int
    Header() http.Header
}
```

`StatusCode() == 0` means "not HTTP-shaped" (e.g. transport failure
before a response was received) and is treated as not retriable.
`Header()` may return `nil` for non-HTTP errors.

The `x/provider/openai` and `x/provider/anthropic` adapters already wrap
`*openai.Error` and `*anthropic.Error` respectively. New adapters only
need a `*retriableError`-like wrapper exposing `StatusCode()` and
`Header()`. The wrapper should also `Unwrap()` to the original SDK error
so `errors.As(err, &sdk.Error{})` continues to work.

## Tracing

When `WithTracer` is configured, the decorator opens a single
`retry.invoke` span per `Invoke` call. Per-attempt transitions are
recorded as `retry.attempt` events on the parent span (no nested
sub-spans). The final attempt count is attached as `retry.attempts` on
the span.

| Span / event | Type | Notes |
|--------------|------|-------|
| `retry.invoke` | span (`SpanKindInternal`) | One per `Invoke` call |
| `retry.attempt` | event | One per failed-attempt transition; carries `retry.attempt_number`, `retry.delay_ms`, `retry.action` |
| `retry.attempts` | attribute | Final attempt count when the call returns |

## When to Use

Use the retry decorator when:

- The upstream LLM provider returns transient errors (5xx, 429) at
  non-trivial rates.
- The call is long enough that one retry meaningfully changes the
  outcome (e.g. a 30-second coding session can absorb a 500ms backoff; a
  200ms single-shot CLI invocation probably should not).
- You want a single place to tune retry behavior across all providers.

Do not use the retry decorator when:

- The inner provider already retries internally (double-retry is
  wasteful and hard to reason about).
- The error is application-level and not transient (4xx, validation
  errors). The default classifier already recognizes these and will
  return them without retrying.
- The artifact stream is unidirectional to a slow consumer and the
  inner provider cannot be cancelled mid-stream. The streaming
  backstop will trip on the first emitted artifact, but the user-
  visible failure will be the same as without retry.

## Limitations

- The decorator retries the entire `Invoke` call, including any
  non-idempotent side effects the inner provider may have. For
  provider APIs, this is generally safe (LLM APIs are idempotent for a
  given request body), but if a future provider adapter does work
  before emitting, that work will be repeated.
- The decorator observes emission via a tee goroutine that forwards to
  the caller's channel. This adds a small per-artifact overhead but
  does not change the back-pressure model — the inner provider still
  blocks on send until the consumer accepts (or the context is
  cancelled).
- The classifier only sees the *final* error of each attempt, not the
  intermediate emissions. A 5xx that arrives mid-stream (after a
  successful 200 chunk) will be reported as the error, but the partial
  stream has already been delivered to the consumer.
