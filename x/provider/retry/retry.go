package retry

import (
	"context"
	"net/http"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/state"

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

// DefaultClassifier is the default classifier. It is exported so
// callers can wrap it (e.g. to add logging) without re-implementing
// the HTTPError-based policy.
//
// Stub: Task 3 replaces this body with the real HTTPError-based
// implementation that recognises 5xx, 429, and Retry-After.
func DefaultClassifier(err error) (Action, time.Duration) {
	_ = err
	return ActionFail, 0
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
	return p
}

// Invoke delegates to the inner provider. Task 3 replaces this with
// the retry loop.
func (p *Provider) Invoke(ctx context.Context, s state.State, spec models.Spec, ch chan<- artifact.Artifact, oo ...provider.InvokeOption) error {
	return p.inner.Invoke(ctx, s, spec, ch, oo...)
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
