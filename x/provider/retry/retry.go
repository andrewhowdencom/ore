package retry

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/state"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Action is the verdict of a ClassifyFunc.
type Action int

const (
	// ActionRetry indicates the error is transient; the decorator
	// should wait for the classifier-supplied delay (or the backoff)
	// and re-invoke the inner provider.
	ActionRetry Action = iota

	// ActionFail indicates the error is not transient; the decorator
	// should return the error immediately without retrying.
	ActionFail
)

// HTTPError is the SDK-agnostic error shape the retry package
// understands. Provider adapters wrap their underlying SDK errors in
// types that implement this interface, so the default classifier can
// recognise 5xx/429 responses without retry importing any vendor SDK.
//
// Adapters should implement Unwrap() on the wrapper so the original
// SDK error remains reachable via errors.As.
type HTTPError interface {
	error
	// StatusCode returns the HTTP status code. 0 means the error is not
	// HTTP-shaped (e.g. transport failure before a response was
	// received) and is treated as not retriable.
	StatusCode() int
	// Header returns the HTTP response headers, used to read
	// Retry-After. nil is acceptable for non-HTTP errors.
	Header() http.Header
}

// ClassifyFunc inspects an error and returns the action to take,
// plus an optional delay (e.g. from Retry-After). A zero delay means
// "use the backoff's NextBackOff()".
type ClassifyFunc func(err error) (Action, time.Duration)

// Option configures a Provider.
type Option func(*options)

// Provider is a provider.Provider decorator that retries transient
// upstream failures. The zero value is not usable; construct one with
// New.
type Provider struct {
	inner provider.Provider
	opts  options
}

// options holds the resolved configuration of a Provider. Fields are
// populated by Option functions; sensible defaults are set by New.
type options struct {
	maxAttempts     int
	baseDelay       time.Duration
	maxDelay        time.Duration
	honorRetryAfter bool
	classify        ClassifyFunc
	tracer          trace.Tracer
}

// DefaultClassifier is the policy used when no classifier is supplied
// via WithClassifier. It recognises HTTP 5xx and 429 responses, and
// reads Retry-After to produce a delay hint. Non-HTTP errors and
// other 4xx codes return ActionFail.
//
// The function is exported so callers can wrap it (e.g. to add
// logging) without re-implementing the HTTPError-based policy.
func DefaultClassifier(err error) (Action, time.Duration) {
	var he HTTPError
	if !errors.As(err, &he) {
		return ActionFail, 0
	}
	status := he.StatusCode()
	// 5xx: server error. Always retriable.
	if status >= 500 && status < 600 {
		return ActionRetry, parseRetryAfter(he.Header())
	}
	// 429: too many requests. Always retriable; Retry-After is
	// conventionally present.
	if status == http.StatusTooManyRequests {
		return ActionRetry, parseRetryAfter(he.Header())
	}
	return ActionFail, 0
}

// parseRetryAfter reads the Retry-After header. The HTTP/1.1 spec
// allows two formats: delta-seconds (a non-negative integer) or
// HTTP-date. Both are handled. A header that is missing, malformed,
// or in the past is reported as zero; the caller will fall back to
// the backoff schedule.
func parseRetryAfter(h http.Header) time.Duration {
	if h == nil {
		return 0
	}
	ra := h.Get("Retry-After")
	if ra == "" {
		return 0
	}
	// Form 1: delta-seconds.
	if secs, err := strconv.Atoi(ra); err == nil && secs >= 0 {
		d := time.Duration(secs) * time.Second
		if d < retryAfterFloor {
			return retryAfterFloor
		}
		return d
	}
	// Form 2: HTTP-date.
	if t, err := http.ParseTime(ra); err == nil {
		if d := time.Until(t); d > 0 {
			if d < retryAfterFloor {
				return retryAfterFloor
			}
			return d
		}
	}
	return 0
}

// New returns a Provider that wraps inner. The decorator retries
// transient upstream failures (5xx, 429) per the default classifier.
// Use the With* options to override defaults.
func New(inner provider.Provider, opts ...Option) *Provider {
	p := &Provider{
		inner: inner,
		opts: options{
			maxAttempts:     3,
			baseDelay:       500 * time.Millisecond,
			maxDelay:        30 * time.Second,
			honorRetryAfter: true,
			classify:        DefaultClassifier,
		},
	}
	for _, opt := range opts {
		opt(&p.opts)
	}
	if p.opts.maxAttempts < 1 {
		// Negative or zero maxAttempts is meaningless; the
		// decorator still makes one attempt and then returns
		// the error. This is the documented "<= 1 disables
		// retry" behavior; normalising here means the
		// Invoke loop never has to special-case 0.
		p.opts.maxAttempts = 1
	}
	if p.opts.classify == nil {
		p.opts.classify = DefaultClassifier
	}
	return p
}

// Invoke calls the inner provider, classifying any error and
// retrying transient failures up to opts.maxAttempts. The behavior
// is described in the package documentation; the per-attempt
// contract is:
//
//   - The inner provider is called with the original ch. Its
//     emissions are observed via a tee goroutine so the retry
//     policy can apply the streaming backstop (see below).
//   - On a non-nil error, opts.classify decides ActionRetry vs
//     ActionFail. The classifier-supplied delay is honored when
//     opts.honorRetryAfter is true and the delay is non-zero;
//     otherwise the backoff schedule is used.
//   - On ActionRetry, the loop sleeps for the chosen delay (or
//     returns ctx.Err() if the context is cancelled) and re-invokes
//     the inner provider.
//   - On ActionFail, the error is returned immediately.
//
// Streaming backstop: once any artifact has been emitted on the
// current attempt, the loop returns the error rather than retrying.
// A retried attempt would re-emit a partial response, which both
// confuses subscribers and risks double-billing on token-based
// providers. Emission state resets per attempt; the rule applies to
// the current attempt only.
func (p *Provider) Invoke(ctx context.Context, s state.State, spec models.Spec, ch chan<- artifact.Artifact, oo ...provider.InvokeOption) error {
	var span trace.Span
	if p.opts.tracer != nil {
		ctx, span = p.opts.tracer.Start(ctx, "retry.invoke", trace.WithSpanKind(trace.SpanKindInternal))
		defer span.End()
	}

	b := newBackoff(p.opts)

	// attempt indexes the current attempt (1-based). The loop
	// terminates after the configured maxAttempts, or earlier
	// when the classifier returns ActionFail, the streaming
	// backstop fires, or the context is cancelled.
	var lastErr error
	for attempt := 1; attempt <= p.opts.maxAttempts; attempt++ {
		emitted := false

		// Tee goroutine: copy every emitted artifact to the
		// caller's channel and set emitted=true on the first
		// one. The buffer matches the caller's so we do not
		// silently increase back-pressure; the inner provider
		// already does the right thing on a select-vs-ctx.Done
		// send. The goroutine exits when wrapper is closed
		// (after inner.Invoke returns), draining any pending
		// artifacts to the caller.
		wrapper := make(chan artifact.Artifact, cap(ch))
		done := make(chan struct{})
		go func() {
			defer close(done)
			for art := range wrapper {
				emitted = true
				select {
				case ch <- art:
				case <-ctx.Done():
					return
				}
			}
		}()

		err := p.inner.Invoke(ctx, s, spec, wrapper, oo...)
		close(wrapper)
		<-done

		// Success: every artifact has been delivered to the
		// caller (the goroutine's select on ch<-art would have
		// blocked until accepted, so we have full hand-off).
		// Record the final attempt count and return.
		if err == nil {
			if span != nil {
				span.SetAttributes(attribute.Int("retry.attempts", attempt))
			}
			return nil
		}

		lastErr = err

		// Last attempt: do not classify, do not sleep. The
		// error is the final result. We still record the
		// attempt count and mark the span as an error for
		// observability.
		if attempt == p.opts.maxAttempts {
			if span != nil {
				span.SetAttributes(attribute.Int("retry.attempts", attempt))
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			}
			return err
		}

		// Streaming backstop: any emission on the current
		// attempt disqualifies retry. The error is returned
		// verbatim so the caller sees the failure that
		// produced the partial response.
		if emitted {
			if span != nil {
				span.SetAttributes(attribute.Int("retry.attempts", attempt))
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			}
			return err
		}

		// Classifier: the source of truth on retriability.
		// ActionFail returns the error verbatim. The
		// classifier-supplied delay is informational; we use
		// it only if honorRetryAfter is on and the delay is
		// non-zero.
		action, classifierDelay := p.opts.classify(err)
		if action == ActionFail {
			if span != nil {
				span.SetAttributes(attribute.Int("retry.attempts", attempt))
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			}
			return err
		}

		var delay time.Duration
		if p.opts.honorRetryAfter && classifierDelay > 0 {
			delay = classifierDelay
		} else {
			delay = b.NextBackOff()
		}

		// Record the transition for tracing. The event is
		// attached to the parent retry.invoke span so a
		// single trace shows the full retry history without
		// the noise of nested per-attempt spans.
		if span != nil {
			span.AddEvent("retry.attempt", trace.WithAttributes(
				attribute.Int("retry.attempt_number", attempt+1),
				attribute.Int64("retry.delay_ms", delay.Milliseconds()),
				attribute.String("retry.action", "retry"),
			))
		}

		// Sleep with cancellation. We always honour ctx
		// during the backoff so a fast-cancel caller does
		// not get stuck on a 30s delay.
		select {
		case <-ctx.Done():
			if span != nil {
				span.SetAttributes(attribute.Int("retry.attempts", attempt))
				span.RecordError(ctx.Err())
				span.SetStatus(codes.Error, ctx.Err().Error())
			}
			return ctx.Err()
		case <-time.After(delay):
		}
	}

	// Unreachable: the loop returns inside the body on
	// success, ActionFail, the streaming backstop, the last
	// attempt, and ctx cancellation. The "return lastErr"
	// below is defensive in case the loop is ever changed
	// to fall through.
	return lastErr
}

// WithMaxAttempts sets the maximum number of attempts (including the
// initial attempt). A value <= 1 disables retry.
func WithMaxAttempts(n int) Option {
	return func(o *options) { o.maxAttempts = n }
}

// WithBaseDelay sets the initial backoff delay.
func WithBaseDelay(d time.Duration) Option {
	return func(o *options) { o.baseDelay = d }
}

// WithMaxDelay sets the maximum backoff delay.
func WithMaxDelay(d time.Duration) Option {
	return func(o *options) { o.maxDelay = d }
}

// WithHonorRetryAfter controls whether the classifier's Retry-After
// delay is honored over the backoff schedule. Default true.
func WithHonorRetryAfter(b bool) Option {
	return func(o *options) { o.honorRetryAfter = b }
}

// WithClassifier overrides the default classifier.
func WithClassifier(c ClassifyFunc) Option {
	return func(o *options) { o.classify = c }
}

// WithTracer enables OpenTelemetry tracing on the retry loop. When
// unset, the decorator performs no tracing.
func WithTracer(t trace.Tracer) Option {
	return func(o *options) { o.tracer = t }
}

// Compile-time assertion that *Provider satisfies provider.Provider.
var _ provider.Provider = (*Provider)(nil)
