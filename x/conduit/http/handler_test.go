package http

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeBackend is an in-memory implementation of Backend used for
// testing. It maintains an internal session registry and an optional
// list of persisted threads for ListThreads. The registry is
// guarded by a mutex because HTTP handlers run on the server's
// goroutines while the test's main goroutine may also inspect it.
type fakeBackend struct {
	mu       sync.Mutex
	sessions map[string]*session.Session
	threads  []ThreadSummary

	// Hooks for behavior injection in tests.
	createHook func(ctx context.Context, threadID string) (*session.Session, error)
	submitHook func(ctx context.Context, id string, event session.Event) error
}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{
		sessions: make(map[string]*session.Session),
	}
}

func (b *fakeBackend) CreateSession(ctx context.Context, threadID string) (*session.Session, error) {
	if b.createHook != nil {
		return b.createHook(ctx, threadID)
	}
	if threadID != "" {
		return nil, session.ErrSessionNotFound
	}
	sess := session.New("sess-"+randomSuffix(), ledger.NewThread())
	b.mu.Lock()
	b.sessions[sess.ID()] = sess
	b.mu.Unlock()
	return sess, nil
}

func (b *fakeBackend) GetSession(ctx context.Context, id string) (*session.Session, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	s, ok := b.sessions[id]
	if !ok {
		return nil, session.ErrSessionNotFound
	}
	return s, nil
}

func (b *fakeBackend) Submit(ctx context.Context, id string, event session.Event) error {
	if b.submitHook != nil {
		return b.submitHook(ctx, id, event)
	}
	s, err := b.GetSession(ctx, id)
	if err != nil {
		return err
	}
	// Push the event into the session's step so subscribers observe it.
	switch e := event.(type) {
	case session.UserMessageEvent:
		if _, err := s.Submit(ctx, ledger.RoleUser, artifact.Text{Content: e.Content}); err != nil {
			return err
		}
	case session.InterruptEvent:
		// No-op for the fake backend; real engine emits lifecycle.
	}
	s.Emitter().Emit(ctx, loop.LifecycleEvent{Phase: "done", Ctx: event.Context()})
	return nil
}

func (b *fakeBackend) ListThreads(ctx context.Context) ([]ThreadSummary, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]ThreadSummary, len(b.threads))
	copy(out, b.threads)
	return out, nil
}

func (b *fakeBackend) DeleteSession(ctx context.Context, id string) error {
	b.mu.Lock()
	s, ok := b.sessions[id]
	if !ok {
		b.mu.Unlock()
		return session.ErrSessionNotFound
	}
	delete(b.sessions, id)
	b.mu.Unlock()
	_ = s.Close()
	return nil
}

// randomSuffix returns a short unique suffix for tests. It uses time
// to avoid pulling in additional dependencies.
var counter int64

func randomSuffix() string {
	n := time.Now().UnixNano()
	counter++
	return formatNanos(n)
}

func formatNanos(n int64) string {
	var buf [20]byte
	i := len(buf)
	if n == 0 {
		i--
		buf[i] = '0'
	} else {
		for n > 0 {
			i--
			buf[i] = byte('0' + n%10)
			n /= 10
		}
	}
	return string(buf[i:])
}

func newTestHandler(t *testing.T, backend Backend, opts ...Option) *Handler {
	t.Helper()
	c, err := New(backend, opts...)
	require.NoError(t, err)
	return c.(*Handler)
}

func TestNew_ValidatesBackend(t *testing.T) {

	_, err := New(nil)
	if err == nil {
		t.Fatal("New(nil) returned nil; expected error")
	}
}

func TestNew_WithName(t *testing.T) {

	backend := newFakeBackend()
	c, err := New(backend, WithName("my-app"))
	require.NoError(t, err)
	h := c.(*Handler)
	assert.Equal(t, "my-app", h.name)
}

func TestHandler_ServeMux_Routing(t *testing.T) {


	backend := newFakeBackend()
	h := newTestHandler(t, backend, WithoutUI())
	server := httptest.NewServer(h.ServeMux())
	defer server.Close()

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{"create session", "POST", "/sessions", 201},
		{"delete session not found", "DELETE", "/sessions/abc-123", 404},
		{"submit event not found", "POST", "/sessions/abc-123/events", 404},
		{"session events not found", "GET", "/sessions/abc-123/events", 404},
		{"get session not found", "GET", "/sessions/abc-123", 404},
		{"list threads", "GET", "/threads", 200},
		{"get sessions method not allowed", "GET", "/sessions", 405},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := stdhttp.NewRequest(tt.method, server.URL+tt.path, nil)
			require.NoError(t, err)
			resp, err := stdhttp.DefaultClient.Do(req)
			require.NoError(t, err)
			_ = resp.Body.Close()
			assert.Equal(t, tt.wantStatus, resp.StatusCode)
		})
	}
}

func TestCreateSession_NoBodyCreatesEphemeralSession(t *testing.T) {


	backend := newFakeBackend()
	h := newTestHandler(t, backend, WithoutUI())
	server := httptest.NewServer(h.ServeMux())
	defer server.Close()

	resp, err := stdhttp.Post(server.URL+"/sessions", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, 201, resp.StatusCode)

	var data map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&data))
	assert.NotEmpty(t, data["id"])
	assert.Contains(t, data["events_url"], data["id"])
}

func TestCreateSession_AttachThread_404(t *testing.T) {


	backend := newFakeBackend()
	h := newTestHandler(t, backend, WithoutUI())
	server := httptest.NewServer(h.ServeMux())
	defer server.Close()

	body, _ := json.Marshal(map[string]string{"thread_id": "missing"})
	resp, err := stdhttp.Post(server.URL+"/sessions", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 404, resp.StatusCode)
}

func TestDeleteSession_RemovesFromBackend(t *testing.T) {


	backend := newFakeBackend()
	h := newTestHandler(t, backend, WithoutUI())
	server := httptest.NewServer(h.ServeMux())
	defer server.Close()

	// Create a session first.
	createResp, err := stdhttp.Post(server.URL+"/sessions", "application/json", nil)
	require.NoError(t, err)
	defer createResp.Body.Close()
	require.Equal(t, 201, createResp.StatusCode)
	var data map[string]string
	require.NoError(t, json.NewDecoder(createResp.Body).Decode(&data))
	id := data["id"]

	// Delete it.
	req, _ := stdhttp.NewRequest("DELETE", server.URL+"/sessions/"+id, nil)
	delResp, err := stdhttp.DefaultClient.Do(req)
	require.NoError(t, err)
	defer delResp.Body.Close()
	assert.Equal(t, 204, delResp.StatusCode)

	// Confirm it's gone from the backend.
	_, err = backend.GetSession(context.Background(), id)
	assert.Error(t, err)
}

func TestSubmitEvent_UserMessageReturns202(t *testing.T) {


	backend := newFakeBackend()
	h := newTestHandler(t, backend, WithoutUI())
	server := httptest.NewServer(h.ServeMux())
	defer server.Close()

	createResp, err := stdhttp.Post(server.URL+"/sessions", "application/json", nil)
	require.NoError(t, err)
	defer createResp.Body.Close()
	var data map[string]string
	require.NoError(t, json.NewDecoder(createResp.Body).Decode(&data))
	id := data["id"]

	body, _ := json.Marshal(map[string]string{"kind": "user_message", "content": "hi"})
	resp, err := stdhttp.Post(server.URL+"/sessions/"+id+"/events", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 202, resp.StatusCode)
}

func TestSubmitEvent_InterruptReturns202(t *testing.T) {


	backend := newFakeBackend()
	h := newTestHandler(t, backend, WithoutUI())
	server := httptest.NewServer(h.ServeMux())
	defer server.Close()

	createResp, err := stdhttp.Post(server.URL+"/sessions", "application/json", nil)
	require.NoError(t, err)
	defer createResp.Body.Close()
	var data map[string]string
	require.NoError(t, json.NewDecoder(createResp.Body).Decode(&data))
	id := data["id"]

	body, _ := json.Marshal(map[string]string{"kind": "interrupt"})
	resp, err := stdhttp.Post(server.URL+"/sessions/"+id+"/events", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 202, resp.StatusCode)
}

func TestSubmitEvent_UnknownKindReturns400(t *testing.T) {


	backend := newFakeBackend()
	h := newTestHandler(t, backend, WithoutUI())
	server := httptest.NewServer(h.ServeMux())
	defer server.Close()

	createResp, err := stdhttp.Post(server.URL+"/sessions", "application/json", nil)
	require.NoError(t, err)
	defer createResp.Body.Close()
	var data map[string]string
	require.NoError(t, json.NewDecoder(createResp.Body).Decode(&data))
	id := data["id"]

	body, _ := json.Marshal(map[string]string{"kind": "bogus"})
	resp, err := stdhttp.Post(server.URL+"/sessions/"+id+"/events", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 400, resp.StatusCode)
}

func TestSessionEvents_StreamsSSE(t *testing.T) {


	backend := newFakeBackend()
	h := newTestHandler(t, backend, WithoutUI())
	server := httptest.NewServer(h.ServeMux())
	defer server.Close()

	createResp, err := stdhttp.Post(server.URL+"/sessions", "application/json", nil)
	require.NoError(t, err)
	defer createResp.Body.Close()
	var data map[string]string
	require.NoError(t, json.NewDecoder(createResp.Body).Decode(&data))
	id := data["id"]

	// Cancel the SSE request when the test ends so the server-side
	// handler exits and httptest.Server.Close returns promptly.
	sseCtx, sseCancel := context.WithCancel(context.Background())
	defer sseCancel()

	// Signal that the SSE handler has wired its subscription
	// (the first SSE comment line has been received). The main
	// thread waits for this before submitting so events do not
	// race the SSE handler's Subscribe call.
	sseReady := make(chan struct{})

	eventsCh := make(chan string, 8)
	errCh := make(chan error, 1)
	go func() {
		req, _ := stdhttp.NewRequestWithContext(sseCtx, "GET", server.URL+"/sessions/"+id+"/events", nil)
		resp, err := stdhttp.DefaultClient.Do(req)
		if err != nil {
			errCh <- err
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			errCh <- errors.New("unexpected status: " + resp.Status)
			return
		}
		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err != io.EOF {
					errCh <- err
				}
				return
			}
			line = strings.TrimRight(line, "\n")
			// The first non-empty line is the ": connected"
			// comment. Signal that SSE is wired up so the main
			// thread can submit.
			if strings.HasPrefix(line, ":") && !sseSignaled(sseReady) {
				close(sseReady)
			}
			if strings.HasPrefix(line, "data: ") {
				eventsCh <- strings.TrimPrefix(line, "data: ")
			}
		}
	}()

	// Wait for the SSE handler to subscribe before submitting.
	select {
	case <-sseReady:
	case err := <-errCh:
		t.Fatalf("SSE setup failed: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for SSE to become ready")
	}

	// Submit a user message; the fake backend emits a TurnCompleteEvent
	// (via Submit) and a LifecycleEvent{Phase:"done"}.
	body, _ := json.Marshal(map[string]string{"kind": "user_message", "content": "hi"})
	resp, err := stdhttp.Post(server.URL+"/sessions/"+id+"/events", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, 202, resp.StatusCode)

	// Read events until we see the lifecycle "done".
	deadline := time.After(3 * time.Second)
	gotDone := false
	for !gotDone {
		select {
		case payload := <-eventsCh:
			var evt map[string]interface{}
			require.NoError(t, json.Unmarshal([]byte(payload), &evt))
			if evt["kind"] == "lifecycle" {
				if le, ok := evt["phase"].(string); ok && le == "done" {
					gotDone = true
				}
			}
		case err := <-errCh:
			t.Fatalf("SSE reader failed: %v", err)
		case <-deadline:
			t.Fatal("timed out waiting for lifecycle done event")
		}
	}
}

// sseSignaled reports whether a sync.Once-style channel has been
// closed. Used to ensure a single close-signal in the SSE reader
// goroutine.
func sseSignaled(ch chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func TestListThreads_PaginationAndCursor(t *testing.T) {


	backend := newFakeBackend()
	// Seed a synthetic thread list (no junk dependency).
	backend.threads = []ThreadSummary{
		{ID: "t1", LastAt: time.Date(2024, 1, 1, 0, 0, 3, 0, time.UTC)},
		{ID: "t2", LastAt: time.Date(2024, 1, 1, 0, 0, 2, 0, time.UTC)},
		{ID: "t3", LastAt: time.Date(2024, 1, 1, 0, 0, 1, 0, time.UTC)},
	}

	h := newTestHandler(t, backend, WithoutUI())
	server := httptest.NewServer(h.ServeMux())
	defer server.Close()

	resp, err := stdhttp.Get(server.URL + "/threads")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, 200, resp.StatusCode)

	var body struct {
		Threads []struct {
			ID      string `json:"id"`
			Preview string `json:"preview,omitempty"`
			LastAt  string `json:"last_at,omitempty"`
		} `json:"threads"`
		NextCursor string `json:"next_cursor"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))

	require.Len(t, body.Threads, 3)
	// Default sort is (last activity desc, id asc) within the same
	// timestamp. Since IDs are unique here and timestamps differ, the
	// first item should be the most recent.
	assert.Equal(t, "t1", body.Threads[0].ID)
	assert.Empty(t, body.NextCursor, "no next cursor when all items fit in one page")
}

func TestStart_ContextCancel(t *testing.T) {


	backend := newFakeBackend()
	c, err := New(backend, WithAddr(":0"))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- c.Start(ctx)
	}()

	time.Sleep(50 * time.Millisecond)

	cancel()
	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return within 2 seconds after context cancellation")
	}
}

func TestWriteJSONError_BodyShape(t *testing.T) {


	backend := newFakeBackend()
	h := newTestHandler(t, backend, WithoutUI())
	server := httptest.NewServer(h.ServeMux())
	defer server.Close()

	resp, err := stdhttp.Post(server.URL+"/sessions/does-not-exist/events", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, 404, resp.StatusCode)

	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "session not found", body["error"])
}
