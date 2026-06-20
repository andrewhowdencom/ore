package retry

import (
	"time"

	"github.com/cenkalti/backoff/v5"
)

// newBackoff builds the exponential-backoff configuration used by the
// retry loop. The instance is created fresh per Invoke so each call
// starts from the same baseline (caller cannot accidentally inherit
// drift from a prior call's NextBackOff state).
//
// Why we own the loop rather than calling backoff.Retry directly:
//
//   - backoff.Retry would terminate on MaxElapsedTime, but we have no
//     use for that — termination is governed by our own MaxAttempts
//     so the per-call budget is explicit.
//   - backoff.Retry is generic in Go 1.18+; pulling it in would force
//     the decorator to expose a typed return value, but Invoke returns
//     only an error.
//
// Settings:
//
//   - InitialInterval = baseDelay. The first retry waits ~baseDelay.
//   - MaxInterval = maxDelay. The schedule caps at ~maxDelay.
//   - Multiplier = 2. The interval doubles on each attempt.
//   - RandomizationFactor = 0.5. Equal jitter: actual delay is in
//     [interval*0.5, interval*1.5]. This is the "full jitter" half —
//     the other half — and matches the value used by AWS-style
//     backoff libraries as the de-facto default.
func newBackoff(o options) *backoff.ExponentialBackOff {
	b := backoff.NewExponentialBackOff()
	b.InitialInterval = o.baseDelay
	b.MaxInterval = o.maxDelay
	b.RandomizationFactor = 0.5
	b.Multiplier = 2
	// MaxElapsedTime is not exposed on the struct in backoff/v5; the
	// library uses it only when called via backoff.Retry. We do not
	// call backoff.Retry, so termination is governed entirely by
	// options.maxAttempts.
	return b
}

// retryAfterFloor is the minimum Retry-After we honor from the wire.
// Smaller values are clamped up to this floor because a sub-second
// backoff during a 429 storm is indistinguishable from no backoff
// and only widens the thundering-herd window.
const retryAfterFloor = 100 * time.Millisecond
