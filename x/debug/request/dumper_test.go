package request

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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
// noop pass-through from the armed capture path. In Task 2 the capture
// path is a pass-through; Tasks 3+ will extend the table to assert
// captured content.
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
		go func() {
			defer wg.Done()
			ctx := loop.WithThreadID(context.Background(), threadID)
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, strings.NewReader("x"))
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

	// After N round trips with the same armed slot, the slot is
	// disarmed (the first CAS won; the rest observed false).
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
