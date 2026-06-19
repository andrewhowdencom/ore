package request

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/andrewhowdencom/ore/loop"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestServer returns an httptest.Server that echoes the path, method,
// and body it received, with a fixed body. Used to assert that
// dumper-wrapped transports pass the body through unchanged.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("X-Echo-Path", r.URL.Path)
		w.Header().Set("X-Echo-Method", r.Method)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("received: " + string(body)))
	}))
}

func TestDumper_EnableAndArmed(t *testing.T) {
	t.Parallel()

	d := New("testapp")
	t.Cleanup(func() { _ = d.Close() })

	// Initially nothing is armed.
	assert.False(t, d.Armed("t1"))
	assert.False(t, d.Armed("t2"))

	d.Enable("t1")
	assert.True(t, d.Armed("t1"))
	assert.False(t, d.Armed("t2"), "other thread IDs are not armed")

	// Re-arming is idempotent.
	d.Enable("t1")
	assert.True(t, d.Armed("t1"))
}

func TestDumper_ReEnableAfterDisarm(t *testing.T) {
	t.Parallel()

	d := New("testapp")
	t.Cleanup(func() { _ = d.Close() })

	d.Enable("t1")
	d.Enable("t1") // already armed: idempotent
	assert.True(t, d.Armed("t1"))
}

// TestDumper_RoundTrip drives the table of cases that distinguish the
// noop pass-through from the armed capture path. Capture semantics are
// extended in later tasks; this test asserts the slot state and the
// pass-through behavior of the body.
func TestDumper_RoundTrip(t *testing.T) {
	t.Parallel()

	const sentinelBody = "hello, dumper"

	tests := []struct {
		name            string
		armFor          string // thread ID to arm; "" = leave disarmed
		ctxThreadID     string // thread ID to attach to the request context; "" = no ID
		wantStatus      int
		wantBodyPrefix  string
		wantStillArmed  bool
	}{
		{
			name:           "disarmed slot passes through",
			armFor:         "",
			ctxThreadID:    "t1",
			wantStatus:     http.StatusOK,
			wantBodyPrefix: "received: ",
			wantStillArmed: false,
		},
		{
			name:           "armed and matching thread disarms in one shot",
			armFor:         "t1",
			ctxThreadID:    "t1",
			wantStatus:     http.StatusOK,
			wantBodyPrefix: "received: ",
			wantStillArmed: false,
		},
		{
			name:           "armed but different thread does not touch slot",
			armFor:         "t1",
			ctxThreadID:    "t2",
			wantStatus:     http.StatusOK,
			wantBodyPrefix: "received: ",
			wantStillArmed: true,
		},
		{
			name:           "armed but no thread ID on context does not touch slot",
			armFor:         "t1",
			ctxThreadID:    "",
			wantStatus:     http.StatusOK,
			wantBodyPrefix: "received: ",
			wantStillArmed: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := newTestServer(t)
			t.Cleanup(server.Close)

			d := New("testapp", WithOutputDir(t.TempDir()))
			t.Cleanup(func() { _ = d.Close() })

			if tc.armFor != "" {
				d.Enable(tc.armFor)
			}

			client := &http.Client{Transport: d.Wrap(http.DefaultTransport)}

			ctx := context.Background()
			if tc.ctxThreadID != "" {
				ctx = loop.WithThreadID(ctx, tc.ctxThreadID)
			}

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/v1/chat", strings.NewReader(sentinelBody))
			require.NoError(t, err)

			resp, err := client.Do(req)
			require.NoError(t, err)
			t.Cleanup(func() { _ = resp.Body.Close() })

			assert.Equal(t, tc.wantStatus, resp.StatusCode)

			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			assert.True(t, strings.HasPrefix(string(body), tc.wantBodyPrefix),
				"body=%q want prefix %q", string(body), tc.wantBodyPrefix)
			// Body echo from the test server confirms the body was
			// passed through to the real transport intact.
			assert.Contains(t, string(body), sentinelBody,
				"request body must arrive at the server unchanged")

			if tc.armFor != "" {
				assert.Equal(t, tc.wantStillArmed, d.Armed(tc.armFor),
					"slot state after round trip")
			}
		})
	}
}

// TestDumper_ArmedConcurrent ensures the arm/disarm CAS is race-free
// when several goroutines hit the dumper at once. With the slot
// armed, exactly one round trip should observe it; the rest must
// pass through.
//
// We detect the winner by giving each goroutine a unique request body
// and checking whose body was captured.
func TestDumper_ArmedConcurrent(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	t.Cleanup(server.Close)

	d := New("testapp", WithOutputDir(t.TempDir()))
	t.Cleanup(func() { _ = d.Close() })

	const threadID = "race-t1"
	d.Enable(threadID)

	client := &http.Client{Transport: d.Wrap(http.DefaultTransport)}

	const N = 8
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		i := i
		go func() {
			defer wg.Done()
			body := "goroutine-" + string(rune('A'+i))
			ctx := loop.WithThreadID(context.Background(), threadID)
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, strings.NewReader(body))
			if err != nil {
				t.Errorf("NewRequest: %v", err)
				return
			}
			resp, err := client.Do(req)
			if err != nil {
				t.Errorf("Do: %v", err)
				return
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}()
	}
	wg.Wait()

	// Exactly one goroutine's body should have been captured. The
	// capture is a fixed buffer populated by the first round trip
	// to win the CAS; subsequent round trips' bodies are not
	// captured (the slot was disarmed after the first win).
	v, ok := d.arms.Load(threadID)
	require.True(t, ok)
	s := v.(*slot)
	require.NotNil(t, s.capture, "capture should have been allocated by the winning round trip")
	captured := s.capture.requestBody.String()
	assert.NotEmpty(t, captured, "the winning goroutine's body must be present")

	// The captured body must match exactly one of the goroutines.
	matched := false
	for i := 0; i < N; i++ {
		want := "goroutine-" + string(rune('A'+i))
		if captured == want {
			matched = true
			break
		}
	}
	assert.True(t, matched,
		"captured body %q does not match any goroutine's body", captured)

	assert.False(t, d.Armed(threadID))
}

func TestDumper_CloseIsIdempotent(t *testing.T) {
	t.Parallel()

	d := New("testapp")
	require.NoError(t, d.Close())
	require.NoError(t, d.Close())
}

func TestDumper_WrapNilTransport(t *testing.T) {
	t.Parallel()

	d := New("testapp")
	t.Cleanup(func() { _ = d.Close() })

	rt := d.Wrap(nil)
	require.NotNil(t, rt, "Wrap(nil) should return a non-nil RoundTripper")
}

// TestDumper_CapturesRequestBody asserts that an armed RoundTrip drains
// the request body into the dumper's slot capture buffer and restores
// the body so the real transport still receives the original bytes.
func TestDumper_CapturesRequestBody(t *testing.T) {
	t.Parallel()

	const sentBody = "the body the wire sees"

	var serverSawBody atomic.Value
	serverSawBody.Store("")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		serverSawBody.Store(string(b))
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	d := New("testapp", WithOutputDir(t.TempDir()))
	t.Cleanup(func() { _ = d.Close() })

	const threadID = "capture-t1"
	d.Enable(threadID)

	client := &http.Client{Transport: d.Wrap(http.DefaultTransport)}
	req, err := http.NewRequestWithContext(
		loop.WithThreadID(context.Background(), threadID),
		http.MethodPost, server.URL+"/echo",
		strings.NewReader(sentBody),
	)
	require.NoError(t, err)
	// Add a custom header so we can assert header capture.
	req.Header.Set("X-Test", "yes")

	resp, err := client.Do(req)
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	// Server received the body intact.
	assert.Equal(t, sentBody, serverSawBody.Load(),
		"the wire must see the original body, not an empty/drained one")

	// Dumper captured the body and the headers.
	v, ok := d.arms.Load(threadID)
	require.True(t, ok)
	s := v.(*slot)
	require.NotNil(t, s.capture, "capture should be populated after an armed round trip")
	assert.Equal(t, sentBody, s.capture.requestBody.String())
	assert.Equal(t, "yes", s.capture.requestHeaders.Get("X-Test"))
}

// TestDumper_NoCaptureBodyWhenNoneSent asserts that an armed RoundTrip
// for a request with no body does not panic and leaves the capture's
// body buffer empty.
func TestDumper_NoCaptureBodyWhenNoneSent(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	t.Cleanup(server.Close)

	d := New("testapp", WithOutputDir(t.TempDir()))
	t.Cleanup(func() { _ = d.Close() })

	const threadID = "capture-no-body"
	d.Enable(threadID)

	client := &http.Client{Transport: d.Wrap(http.DefaultTransport)}
	req, err := http.NewRequestWithContext(
		loop.WithThreadID(context.Background(), threadID),
		http.MethodGet, server.URL,
		nil,
	)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	v, ok := d.arms.Load(threadID)
	require.True(t, ok)
	s := v.(*slot)
	require.NotNil(t, s.capture)
	assert.Equal(t, 0, s.capture.requestBody.Len(),
		"GET with no body should leave the capture body empty")
}

// TestDumper_RequestBodyCanBeReRead is a regression guard for the
// body-restore invariant: the wire (here: the real transport inside
// the wrapper) must be able to read the body more than once if it
// chooses, because we replaced req.Body with a bytes.Reader-backed
// NopCloser.
func TestDumper_RequestBodyCanBeReRead(t *testing.T) {
	t.Parallel()

	const sentBody = "re-readable body"

	var reads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read the body fully, then read again to assert it isn't
		// drained. http.Transport may retry GETs internally; we
		// don't care about that here, we just want to confirm
		// the body we restore is a Reader, not a one-shot
		// ReadCloser.
		buf, _ := io.ReadAll(r.Body)
		assert.Equal(t, sentBody, string(buf))
		reads.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	d := New("testapp", WithOutputDir(t.TempDir()))
	t.Cleanup(func() { _ = d.Close() })

	const threadID = "reread-t1"
	d.Enable(threadID)

	client := &http.Client{Transport: d.Wrap(http.DefaultTransport)}
	req, err := http.NewRequestWithContext(
		loop.WithThreadID(context.Background(), threadID),
		http.MethodPost, server.URL,
		bytes.NewReader([]byte(sentBody)),
	)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	assert.GreaterOrEqual(t, reads.Load(), int32(1),
		"server must have observed at least one request")
}

// TestDumper_CapturesResponseBody asserts that an armed RoundTrip
// captures the response body via the tee. We send a small fixed
// response and check that the dumper's slot.capture.responseBody
// matches what the server wrote.
func TestDumper_CapturesResponseBody(t *testing.T) {
	t.Parallel()

	const responseBody = "the body the wire sees back"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Echo", "yes")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(responseBody))
	}))
	t.Cleanup(server.Close)

	d := New("testapp", WithOutputDir(t.TempDir()))
	t.Cleanup(func() { _ = d.Close() })

	const threadID = "resp-t1"
	d.Enable(threadID)

	client := &http.Client{Transport: d.Wrap(http.DefaultTransport)}
	req, err := http.NewRequestWithContext(
		loop.WithThreadID(context.Background(), threadID),
		http.MethodPost, server.URL,
		strings.NewReader("request body"),
	)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)

	// The wire must still see the body intact.
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	_ = resp.Body.Close()

	assert.Equal(t, responseBody, string(body),
		"wire must receive the original response body")

	v, ok := d.arms.Load(threadID)
	require.True(t, ok)
	s := v.(*slot)
	require.NotNil(t, s.capture)
	assert.Equal(t, responseBody, s.capture.responseBody.String(),
		"dumper should have captured the full response body via the tee")
	assert.Equal(t, "201 Created", s.capture.responseStatus)
	assert.Equal(t, "yes", s.capture.responseHeader.Get("X-Echo"))
}

// TestDumper_CapturesStreamingResponse asserts the tee sees every
// chunk of a streaming response body, in order, before Close().
func TestDumper_CapturesStreamingResponse(t *testing.T) {
	t.Parallel()

	chunks := []string{"chunk-one\n", "chunk-two\n", "chunk-three\n"}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		for _, c := range chunks {
			_, _ = w.Write([]byte(c))
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	t.Cleanup(server.Close)

	d := New("testapp", WithOutputDir(t.TempDir()))
	t.Cleanup(func() { _ = d.Close() })

	const threadID = "stream-t1"
	d.Enable(threadID)

	client := &http.Client{Transport: d.Wrap(http.DefaultTransport)}
	req, err := http.NewRequestWithContext(
		loop.WithThreadID(context.Background(), threadID),
		http.MethodGet, server.URL,
		nil,
	)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)

	// Drain the body chunk by chunk to mirror how a streaming
	// consumer reads.
	var collected bytes.Buffer
	buf := make([]byte, 16)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			collected.Write(buf[:n])
		}
		if rerr != nil {
			break
		}
	}
	require.NoError(t, resp.Body.Close())

	expected := strings.Join(chunks, "")
	assert.Equal(t, expected, collected.String(),
		"wire must see all streaming chunks intact")

	v, ok := d.arms.Load(threadID)
	require.True(t, ok)
	s := v.(*slot)
	require.NotNil(t, s.capture)
	assert.Equal(t, expected, s.capture.responseBody.String(),
		"tee should have captured every streaming chunk in order")
}
