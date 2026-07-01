package retry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/ledger"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// scriptProvider is a programmable provider used to drive the
// decorator through every interesting case. The plan for each attempt
// is a closure that:
//
//   - Receives the per-attempt artifact channel.
//   - Optionally writes artifacts to it (used by the streaming
//     backstop test).
//   - Returns the per-attempt error.
//
// The provider is goroutine-safe: the decorator calls it serially
// from a single goroutine in these tests, but the field set is
// guarded for clarity.
type scriptProvider struct {
	mu      sync.Mutex
	plan    []func(ch chan<- artifact.Artifact) error
	calls   int32
	lastErr error
}

func (s *scriptProvider) Invoke(_ context.Context, _ ledger.State, _ models.Spec, ch chan<- artifact.Artifact, _ ...provider.InvokeOption) error {
	idx := int(atomic.AddInt32(&s.calls, 1)) - 1
	s.mu.Lock()
	if idx < len(s.plan) {
		step := s.plan[idx]
		s.mu.Unlock()
		return step(ch)
	}
	// Out-of-bounds: return whatever the last error was, falling
	// back to a default. Tests should size the plan to match
	// the expected attempt count.
	s.mu.Unlock()
	if s.lastErr != nil {
		return s.lastErr
	}
	return errors.New("scriptProvider: out of plan")
}

// httpErr is a test-only HTTPError that records the status code
// and headers of a simulated upstream response.
type httpErr struct {
	status  int
	header  http.Header
	message string
}

func (e *httpErr) Error() string {
	return fmt.Sprintf("http %d: %s", e.status, e.message)
}

func (e *httpErr) StatusCode() int      { return e.status }
func (e *httpErr) Header() http.Header { return e.header }
func (e *httpErr) Unwrap() error       { return nil }

// fastOpts returns retry options that minimise the backoff so the
// tests do not stall. Tests that need to assert on the delay
// should pass an explicit classifier and skip these options.
func fastOpts() []Option {
	return []Option{
		WithBaseDelay(1 * time.Millisecond),
		WithMaxDelay(1 * time.Millisecond),
		WithHonorRetryAfter(false),
	}
}

func newState() *ledger.Buffer {
	s := &ledger.Buffer{}
	s.Append(ledger.RoleUser, artifact.Text{Content: "hi"})
	return s
}

// TestProvider_5xx_ThenSuccess: a transient 503 is retried, the
// second attempt returns success.
func TestProvider_5xx_ThenSuccess(t *testing.T) {
	t.Parallel()

	inner := &scriptProvider{
		plan: []func(chan<- artifact.Artifact) error{
			func(_ chan<- artifact.Artifact) error {
				return &httpErr{status: 503, message: "unavailable"}
			},
			func(ch chan<- artifact.Artifact) error {
				ch <- artifact.TextDelta{Content: "ok"}
				return nil
			},
		},
	}

	p := New(inner, fastOpts()...)
	ch := make(chan artifact.Artifact, 8)
	require.NoError(t, p.Invoke(t.Context(), newState(), models.Spec{Name: "x"}, ch))
	close(ch)

	var out []artifact.Artifact
	for a := range ch {
		out = append(out, a)
	}
	require.Len(t, out, 1)
	assert.Equal(t, "text_delta", out[0].Kind())
	assert.Equal(t, "ok", out[0].(artifact.TextDelta).Content)
	assert.Equal(t, int32(2), atomic.LoadInt32(&inner.calls))
}

// TestProvider_429_WithRetryAfter: a 429 with a Retry-After
// header is retried; the second attempt succeeds. The
// WithHonorRetryAfter default is on, so the 1-second
// classifier-supplied delay is honored.
func TestProvider_429_WithRetryAfter(t *testing.T) {
	t.Parallel()

	inner := &scriptProvider{
		plan: []func(chan<- artifact.Artifact) error{
			func(_ chan<- artifact.Artifact) error {
				return &httpErr{
					status: http.StatusTooManyRequests,
					header: http.Header{"Retry-After": []string{"1"}},
				}
			},
			func(ch chan<- artifact.Artifact) error {
				ch <- artifact.TextDelta{Content: "ok"}
				return nil
			},
		},
	}

	// WithHonorRetryAfter(true) (the default) tells the decorator
	// to use the classifier-supplied delay. The 1-second value
	// from the header is large enough to be observable but the
	// test only asserts on attempt count, not wall-clock
	// duration (to keep the test fast).
	p := New(inner, WithBaseDelay(1*time.Millisecond), WithMaxDelay(1*time.Millisecond))
	ch := make(chan artifact.Artifact, 8)
	require.NoError(t, p.Invoke(t.Context(), newState(), models.Spec{Name: "x"}, ch))
	close(ch)

	for range ch {
	}
	assert.Equal(t, int32(2), atomic.LoadInt32(&inner.calls))
}

// TestProvider_4xx_NoRetry: a 4xx is not transient; the default
// classifier returns ActionFail and the decorator returns
// the error verbatim after one attempt.
func TestProvider_4xx_NoRetry(t *testing.T) {
	t.Parallel()

	inner := &scriptProvider{
		plan: []func(chan<- artifact.Artifact) error{
			func(_ chan<- artifact.Artifact) error {
				return &httpErr{status: 400, message: "bad request"}
			},
		},
	}

	p := New(inner, fastOpts()...)
	ch := make(chan artifact.Artifact, 8)
	err := p.Invoke(t.Context(), newState(), models.Spec{Name: "x"}, ch)
	require.Error(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&inner.calls), "4xx must not retry")
}

// TestProvider_AllAttemptsFail: every attempt returns 5xx; the
// decorator exhausts maxAttempts (3) and returns the final error.
func TestProvider_AllAttemptsFail(t *testing.T) {
	t.Parallel()

	inner := &scriptProvider{
		plan: []func(chan<- artifact.Artifact) error{
			func(_ chan<- artifact.Artifact) error { return &httpErr{status: 503} },
			func(_ chan<- artifact.Artifact) error { return &httpErr{status: 502} },
			func(_ chan<- artifact.Artifact) error { return &httpErr{status: 504} },
		},
	}

	p := New(inner, fastOpts()...)
	ch := make(chan artifact.Artifact, 8)
	err := p.Invoke(t.Context(), newState(), models.Spec{Name: "x"}, ch)
	require.Error(t, err)
	assert.Equal(t, int32(3), atomic.LoadInt32(&inner.calls))

	var he HTTPError
	require.True(t, errors.As(err, &he), "final error must implement retry.HTTPError")
	assert.Equal(t, 504, he.StatusCode(), "final error is the last attempt's error")
}

// TestProvider_EmissionThenFailure: the first attempt emits an
// artifact and then returns a 5xx; the streaming backstop fires
// and the decorator returns the error without retrying. The
// already-emitted artifact is preserved (forwarded to the
// caller's channel).
func TestProvider_EmissionThenFailure(t *testing.T) {
	t.Parallel()

	inner := &scriptProvider{
		plan: []func(chan<- artifact.Artifact) error{
			func(ch chan<- artifact.Artifact) error {
				ch <- artifact.TextDelta{Content: "partial"}
				return &httpErr{status: 503}
			},
			func(_ chan<- artifact.Artifact) error {
				// Should never be called: the streaming
				// backstop suppresses retry once any
				// artifact has been emitted on the
				// current attempt.
				t.Fatal("second attempt must not be made after emission")
				return nil
			},
		},
	}

	p := New(inner, fastOpts()...)
	ch := make(chan artifact.Artifact, 8)
	err := p.Invoke(t.Context(), newState(), models.Spec{Name: "x"}, ch)
	close(ch)
	require.Error(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&inner.calls), "must not retry after emission")

	var out []artifact.Artifact
	for a := range ch {
		out = append(out, a)
	}
	require.Len(t, out, 1, "exactly the partial artifact should reach the caller")
	assert.Equal(t, "partial", out[0].(artifact.TextDelta).Content)
}

// TestProvider_CustomClassifier: a custom classifier that always
// retries is honored by the decorator. This proves the WithClassifier
// escape hatch is wired correctly.
func TestProvider_CustomClassifier(t *testing.T) {
	t.Parallel()

	calls := atomic.Int32{}
	classifier := func(_ error) (Action, time.Duration) {
		calls.Add(1)
		return ActionRetry, 0
	}

	inner := &scriptProvider{
		plan: []func(chan<- artifact.Artifact) error{
			func(_ chan<- artifact.Artifact) error { return errors.New("anything") },
			func(_ chan<- artifact.Artifact) error { return errors.New("anything") },
			func(ch chan<- artifact.Artifact) error {
				ch <- artifact.TextDelta{Content: "ok"}
				return nil
			},
		},
	}

	p := New(inner, append(fastOpts(), WithClassifier(classifier))...)
	ch := make(chan artifact.Artifact, 8)
	require.NoError(t, p.Invoke(t.Context(), newState(), models.Spec{Name: "x"}, ch))
	close(ch)

	for range ch {
	}
	assert.Equal(t, int32(3), atomic.LoadInt32(&inner.calls))
	assert.Equal(t, int32(2), calls.Load(), "classifier should fire once per failed attempt")
}

// TestProvider_WithMaxAttemptsOne: WithMaxAttempts(1) disables
// retry. The single failing attempt returns its error verbatim.
func TestProvider_WithMaxAttemptsOne(t *testing.T) {
	t.Parallel()

	inner := &scriptProvider{
		plan: []func(chan<- artifact.Artifact) error{
			func(_ chan<- artifact.Artifact) error { return &httpErr{status: 503} },
			func(_ chan<- artifact.Artifact) error {
				t.Fatal("second attempt must not be made when MaxAttempts=1")
				return nil
			},
		},
	}

	p := New(inner, append(fastOpts(), WithMaxAttempts(1))...)
	ch := make(chan artifact.Artifact, 8)
	err := p.Invoke(t.Context(), newState(), models.Spec{Name: "x"}, ch)
	require.Error(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&inner.calls))
}

// TestProvider_WithHonorRetryAfterFalse: with WithHonorRetryAfter
// off, the classifier-supplied Retry-After is ignored and the
// backoff schedule is used. The test asserts the attempt still
// happens, which is the contract the option modifies.
func TestProvider_WithHonorRetryAfterFalse(t *testing.T) {
	t.Parallel()

	inner := &scriptProvider{
		plan: []func(chan<- artifact.Artifact) error{
			func(_ chan<- artifact.Artifact) error {
				return &httpErr{
					status: http.StatusTooManyRequests,
					header: http.Header{"Retry-After": []string{"30"}},
				}
			},
			func(ch chan<- artifact.Artifact) error {
				ch <- artifact.TextDelta{Content: "ok"}
				return nil
			},
		},
	}

	// WithHonorRetryAfter(false) + 1ms backoff = total wall time
	// well under 30s. If the option were broken, the test would
	// hang for ~30s before timing out.
	p := New(inner,
		WithBaseDelay(1*time.Millisecond),
		WithMaxDelay(1*time.Millisecond),
		WithHonorRetryAfter(false),
	)
	ch := make(chan artifact.Artifact, 8)
	require.NoError(t, p.Invoke(t.Context(), newState(), models.Spec{Name: "x"}, ch))
	close(ch)
	for range ch {
	}
	assert.Equal(t, int32(2), atomic.LoadInt32(&inner.calls))
}

// TestProvider_Tracing: with WithTracer, a single retry.invoke
// span is opened, retry.attempt events are added for each
// transition, and the final attempt count is on the span.
func TestProvider_Tracing(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	tracer := tracerProvider.Tracer("test")

	inner := &scriptProvider{
		plan: []func(chan<- artifact.Artifact) error{
			func(_ chan<- artifact.Artifact) error { return &httpErr{status: 503} },
			func(_ chan<- artifact.Artifact) error { return &httpErr{status: 502} },
			func(ch chan<- artifact.Artifact) error {
				ch <- artifact.TextDelta{Content: "ok"}
				return nil
			},
		},
	}

	p := New(inner, append(fastOpts(), WithTracer(tracer))...)
	ch := make(chan artifact.Artifact, 8)
	require.NoError(t, p.Invoke(t.Context(), newState(), models.Spec{Name: "x"}, ch))
	close(ch)
	for range ch {
	}

	spans := recorder.Ended()
	require.Len(t, spans, 1, "exactly one retry.invoke span is expected")
	root := spans[0]
	assert.Equal(t, "retry.invoke", root.Name())

	// One event per failed-attempt transition; the final
	// successful attempt is not announced as an event.
	events := root.Events()
	attemptEvents := 0
	for _, e := range events {
		if e.Name == "retry.attempt" {
			attemptEvents++
		}
	}
	assert.Equal(t, 2, attemptEvents, "two retry.attempt events for two failed attempts")

	// Final retry.attempts attribute reflects 3 (initial +
	// two retries).
	var foundAttempts bool
	for _, kv := range root.Attributes() {
		if string(kv.Key) == "retry.attempts" {
			foundAttempts = true
			assert.EqualValues(t, 3, kv.Value.AsInt64())
		}
	}
	assert.True(t, foundAttempts, "retry.attempts attribute must be set")
}

// TestProvider_ContextCancellation: cancellation during the
// backoff sleep returns ctx.Err() promptly. The second attempt
// is not made.
func TestProvider_ContextCancellation(t *testing.T) {
	t.Parallel()

	inner := &scriptProvider{
		plan: []func(chan<- artifact.Artifact) error{
			func(_ chan<- artifact.Artifact) error { return &httpErr{status: 503} },
			func(_ chan<- artifact.Artifact) error {
				t.Fatal("second attempt must not be made after context cancellation")
				return nil
			},
		},
	}

	p := New(inner,
		WithBaseDelay(500*time.Millisecond),
		WithMaxDelay(1*time.Second),
	)

	ctx, cancel := context.WithCancel(t.Context())
	ch := make(chan artifact.Artifact, 8)

	// Cancel after 5ms — well before the 500ms backoff elapses.
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()

	err := p.Invoke(ctx, newState(), models.Spec{Name: "x"}, ch)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, int32(1), atomic.LoadInt32(&inner.calls))
}

// TestProvider_NonHTTPError: a non-HTTP error (plain Go error)
// is not retried; the default classifier returns ActionFail.
// The original error is returned verbatim (errors.Is chain
// holds).
func TestProvider_NonHTTPError(t *testing.T) {
	t.Parallel()

	want := errors.New("plain")
	inner := &scriptProvider{
		plan: []func(chan<- artifact.Artifact) error{
			func(_ chan<- artifact.Artifact) error { return want },
		},
	}

	p := New(inner, fastOpts()...)
	ch := make(chan artifact.Artifact, 8)
	err := p.Invoke(t.Context(), newState(), models.Spec{Name: "x"}, ch)
	require.ErrorIs(t, err, want, "returned error must be the original")
	assert.Equal(t, int32(1), atomic.LoadInt32(&inner.calls))
}

// TestProvider_SuccessFirstAttempt: the success-on-first-try
// path emits the artifact once, makes a single call, and
// returns nil without entering the retry loop.
func TestProvider_SuccessFirstAttempt(t *testing.T) {
	t.Parallel()

	inner := &scriptProvider{
		plan: []func(chan<- artifact.Artifact) error{
			func(ch chan<- artifact.Artifact) error {
				ch <- artifact.TextDelta{Content: "hello"}
				return nil
			},
		},
	}

	p := New(inner, fastOpts()...)
	ch := make(chan artifact.Artifact, 8)
	require.NoError(t, p.Invoke(t.Context(), newState(), models.Spec{Name: "x"}, ch))
	close(ch)

	var out []artifact.Artifact
	for a := range ch {
		out = append(out, a)
	}
	require.Len(t, out, 1)
	assert.Equal(t, "hello", out[0].(artifact.TextDelta).Content)
	assert.Equal(t, int32(1), atomic.LoadInt32(&inner.calls))
}

// TestParseRetryAfter: the parser handles both delta-seconds and
// HTTP-date formats, and returns zero for missing or malformed
// headers. The 100ms floor is also asserted.
func TestParseRetryAfter(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		h    http.Header
		want time.Duration
	}{
		{"nil header", nil, 0},
		{"empty header", http.Header{}, 0},
		{"missing", http.Header{"Other": []string{"x"}}, 0},
		{"delta seconds", http.Header{"Retry-After": []string{"5"}}, 5 * time.Second},
		{"delta zero floors to 100ms", http.Header{"Retry-After": []string{"0"}}, 100 * time.Millisecond},
		{"delta below floor", http.Header{"Retry-After": []string{"0"}}, 100 * time.Millisecond},
		{"malformed number", http.Header{"Retry-After": []string{"abc"}}, 0},
		// Round-trip through second-precision first: the HTTP
		// date format ("Mon, 02 Jan 2006 15:04:05 GMT") has no
		// sub-second component, so a "now" formatted with
		// truncation to the second is unambiguously in the
		// past. We also need to format in UTC: time.Now() is
		// local, but http.TimeFormat's reference time is in
		// UTC, so format() converts to UTC for display. A
		// "1h ago in local" string would be parsed as "1h ago
		// in UTC", which (depending on the local TZ offset) is
		// in the future.
		{"http-date in past", http.Header{"Retry-After": []string{time.Now().UTC().Truncate(time.Second).Add(-time.Hour).Format(http.TimeFormat)}}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseRetryAfter(tc.h)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestDefaultClassifier: the production default classifier's
// table-driven matrix. Each case asserts only the action (and
// not the delay), except for the 429 case which asserts a
// non-zero delay because of the Retry-After header.
func TestDefaultClassifier(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want Action
	}{
		{"plain error", errors.New("x"), ActionFail},
		{"400", &httpErr{status: 400}, ActionFail},
		{"401", &httpErr{status: 401}, ActionFail},
		{"404", &httpErr{status: 404}, ActionFail},
		{"500", &httpErr{status: 500}, ActionRetry},
		{"502", &httpErr{status: 502}, ActionRetry},
		{"503", &httpErr{status: 503}, ActionRetry},
		{"504", &httpErr{status: 504}, ActionRetry},
		{"429 without RA", &httpErr{status: 429, message: "x"}, ActionRetry},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := DefaultClassifier(tc.err)
			assert.Equal(t, tc.want, got, "action")
		})
	}
}

// TestDefaultClassifier_429_Delay: the 429 case returns a delay
// when Retry-After is present. This is the contract callers
// rely on when they set WithHonorRetryAfter(true) (the default).
func TestDefaultClassifier_429_Delay(t *testing.T) {
	t.Parallel()

	err := &httpErr{
		status:  http.StatusTooManyRequests,
		header:  http.Header{"Retry-After": []string{"2"}},
		message: "x",
	}
	_, dly := DefaultClassifier(err)
	assert.GreaterOrEqual(t, dly, 2*time.Second)
}
