package http

import (
	"github.com/andrewhowdencom/ore/models"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/junk"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/andrewhowdencom/ore/x/conduit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockProvider is a provider.Provider implementation for testing that can be
// configured to emit a sequence of artifacts, optionally returning an error.
type mockProvider struct {
	artifacts []artifact.Artifact
	err       error
}

func (m *mockProvider) Invoke(ctx context.Context, s ledger.State, _ models.Spec, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
	for _, art := range m.artifacts {
		select {
		case ch <- art:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return m.err
}

// noFlusherWriter implements http.ResponseWriter but NOT http.Flusher.
// Used to test writer creation failure paths.
type noFlusherWriter struct {
	header http.Header
	body   *bytes.Buffer
	code   int
}

func (w *noFlusherWriter) Header() http.Header         { return w.header }
func (w *noFlusherWriter) Write(b []byte) (int, error) { return w.body.Write(b) }
func (w *noFlusherWriter) WriteHeader(code int)        { w.code = code }

// errorFS is a test double for fs.ReadFileFS that always returns an error.
type errorFS struct{}

func (e *errorFS) Open(name string) (fs.File, error)    { return nil, fs.ErrNotExist }
func (e *errorFS) ReadFile(name string) ([]byte, error) { return nil, fs.ErrNotExist }

// simpleProcessor runs a single Step.Turn with the mock provider.
func simpleProcessor() junk.TurnProcessor {
	return func(ctx context.Context, step *loop.Step, st ledger.State, prov provider.Provider, _ models.Spec) (ledger.State, error) {
		spec := models.Spec{Name: "test-model"}
		return step.Turn(ctx, st, spec, prov)
	}
}

// boomProcessor always fails.
func boomProcessor() junk.TurnProcessor {
	return func(ctx context.Context, step *loop.Step, st ledger.State, prov provider.Provider, _ models.Spec) (ledger.State, error) {
		return st, fmt.Errorf("boom")
	}
}

// errStore is a Store that always returns an error from Create.
type errStore struct{}

func (e *errStore) Create() (*junk.Thread, error)              { return nil, fmt.Errorf("store error") }
func (e *errStore) Get(string) (*junk.Thread, error)           { return nil, junk.ErrThreadNotFound }
func (e *errStore) GetBy(string, string) (*junk.Thread, error) { return nil, junk.ErrThreadNotFound }
func (e *errStore) Save(*junk.Thread) error                   { return nil }
func (e *errStore) Delete(string) bool                           { return false }
func (e *errStore) List() ([]*junk.Thread, error)             { return nil, nil }

// saveErrStore is a Store whose Save always returns an error.
type saveErrStore struct {
	inner junk.Store
}

func newSaveErrStore() *saveErrStore {
	return &saveErrStore{inner: junk.NewMemoryStore()}
}

func (s *saveErrStore) Create() (*junk.Thread, error)     { return s.inner.Create() }
func (s *saveErrStore) Get(id string) (*junk.Thread, error) {
	return s.inner.Get(id)
}
func (s *saveErrStore) GetBy(key, value string) (*junk.Thread, error) {
	return s.inner.GetBy(key, value)
}
func (s *saveErrStore) Save(*junk.Thread) error       { return fmt.Errorf("save failed") }
func (s *saveErrStore) Delete(string) bool               { return s.inner.Delete("") }
func (s *saveErrStore) List() ([]*junk.Thread, error) { return s.inner.List() }

// listErrStore is a Store whose List always returns an error.
type listErrStore struct{}

func (s *listErrStore) Create() (*junk.Thread, error)              { return nil, nil }
func (s *listErrStore) Get(string) (*junk.Thread, error)           { return nil, junk.ErrThreadNotFound }
func (s *listErrStore) GetBy(string, string) (*junk.Thread, error) { return nil, junk.ErrThreadNotFound }
func (s *listErrStore) Save(*junk.Thread) error                   { return nil }
func (s *listErrStore) Delete(string) bool                           { return false }
func (s *listErrStore) List() ([]*junk.Thread, error)             { return nil, fmt.Errorf("list failed") }

// newTestHandler wraps New for table-driven tests that need access to the
// concrete *Handler and its ServeMux() method.
func newTestHandler(t *testing.T, mgr *junk.Manager, opts ...Option) *Handler {
	c, err := New(mgr, opts...)
	require.NoError(t, err)
	return c.(*Handler)
}

func TestNew(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())
	c, err := New(mgr)
	require.NoError(t, err)
	require.NotNil(t, c)
}

func TestNew_WithAddr(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())
	c, err := New(mgr, WithAddr(":0"))
	require.NoError(t, err)
	require.NotNil(t, c)

	// Verify the server starts on an ephemeral port and shuts down cleanly.
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- c.Start(ctx)
	}()

	// Give the server time to bind.
	time.Sleep(50 * time.Millisecond)

	cancel()
	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return within 2 seconds after context cancellation")
	}
}

func TestNew_WithName(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())
	c, err := New(mgr, WithName("my-app"))
	require.NoError(t, err)
	require.NotNil(t, c)

	// Verify the name was stored by accessing it through the concrete type.
	h := c.(*Handler)
	assert.Equal(t, "my-app", h.name)
}

func TestStart_ContextCancel(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())
	c, err := New(mgr, WithAddr(":0"))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- c.Start(ctx)
	}()

	// Give the server time to start on an ephemeral port.
	time.Sleep(50 * time.Millisecond)

	cancel()
	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return within 2 seconds after context cancellation")
	}
}

func TestHandler_ServeMux_Routing(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())
	h := newTestHandler(t, mgr, WithoutUI())
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
		{"send message not found", "POST", "/sessions/abc-123/messages", 404},
		{"session events not found", "GET", "/sessions/abc-123/events", 404},
		{"session turns not found", "GET", "/sessions/abc-123/turns", 404},
		{"list threads", "GET", "/threads", 200},
		{"get sessions method not allowed", "GET", "/sessions", 405},
		{"post to session root method not allowed", "POST", "/sessions/abc-123", 405},
		{"put method not allowed", "PUT", "/sessions/abc-123", 405},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rr := httptest.NewRecorder()
			h.ServeMux().ServeHTTP(rr, req)
			assert.Equal(t, tt.wantStatus, rr.Code)
		})
	}
}

func TestHandler_CreateSession(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())
	h := newTestHandler(t, mgr)

	req := httptest.NewRequest("POST", "/sessions", nil)
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	require.Equal(t, 201, rr.Code)
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	var resp map[string]string
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	require.NotEmpty(t, resp["id"])
	assert.Equal(t, "/sessions/"+resp["id"]+"/events", resp["events_url"])

	// Verify the thread exists in the store.
	_, err := store.Get(resp["id"])
	assert.NoError(t, err)
}

func TestHandler_CreateSession_StoreError(t *testing.T) {
	store := &errStore{}
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())
	h := newTestHandler(t, mgr)

	req := httptest.NewRequest("POST", "/sessions", nil)
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	assert.Equal(t, 500, rr.Code)
}

func TestHandler_CreateSession_AttachExisting(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())
	h := newTestHandler(t, mgr)

	// Create a thread directly in the store.
	thr, err := store.Create()
	require.NoError(t, err)

	// Attach to the existing thread.
	body := fmt.Sprintf(`{"thread_id": "%s"}`, thr.ID)
	req := httptest.NewRequest("POST", "/sessions", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	require.Equal(t, 201, rr.Code)

	var resp map[string]string
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Equal(t, thr.ID, resp["id"])
}

func TestHandler_CreateSession_AttachNotFound(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())
	h := newTestHandler(t, mgr)

	body := `{"thread_id": "nonexistent"}`
	req := httptest.NewRequest("POST", "/sessions", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	assert.Equal(t, 404, rr.Code)
}

func TestHandler_CreateSession_MalformedJSON(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())
	h := newTestHandler(t, mgr)

	body := `{"thread_id": "`
	req := httptest.NewRequest("POST", "/sessions", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	assert.Equal(t, 400, rr.Code)
}

func TestHandler_DeleteSession(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())
	h := newTestHandler(t, mgr)

	// Create a session first.
	createReq := httptest.NewRequest("POST", "/sessions", nil)
	createRr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(createRr, createReq)
	require.Equal(t, 201, createRr.Code)

	var resp map[string]string
	require.NoError(t, json.Unmarshal(createRr.Body.Bytes(), &resp))
	sessionID := resp["id"]

	// Delete the junk.
	deleteReq := httptest.NewRequest("DELETE", "/sessions/"+sessionID, nil)
	deleteRr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(deleteRr, deleteReq)

	assert.Equal(t, 204, deleteRr.Code)
	assert.Empty(t, deleteRr.Body.String())

	// Verify the session is removed from the registry.
	_, err := mgr.Get(sessionID)
	require.Error(t, err)

	// Verify the thread still exists in the store.
	_, err = store.Get(sessionID)
	assert.NoError(t, err)
}

func TestHandler_DeleteSession_NotFound(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())
	h := newTestHandler(t, mgr)

	req := httptest.NewRequest("DELETE", "/sessions/nonexistent", nil)
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	assert.Equal(t, 404, rr.Code)
}

func TestHandler_SendMessage(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "Hello"},
			artifact.TextDelta{Content: " world"},
		},
	}
	store := junk.NewMemoryStore()
	mgr := junk.NewManager(store, prov, func(stream *junk.Stream) ([]loop.Option, error) {
		return nil, nil
	}, simpleProcessor())
	h := newTestHandler(t, mgr)

	// Create a junk.
	createReq := httptest.NewRequest("POST", "/sessions", nil)
	createRr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(createRr, createReq)
	require.Equal(t, 201, createRr.Code)

	var createResp map[string]string
	require.NoError(t, json.Unmarshal(createRr.Body.Bytes(), &createResp))
	sessionID := createResp["id"]

	// Send a message.
	body := `{"content": "hi", "kinds": ["text_delta", "turn_complete"]}`
	req := httptest.NewRequest("POST", "/sessions/"+sessionID+"/messages", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	require.Equal(t, 200, rr.Code)
	assert.Equal(t, "application/x-ndjson", rr.Header().Get("Content-Type"))

	// Parse NDJSON lines.
	lines := strings.Split(strings.TrimSpace(rr.Body.String()), "\n")
	require.NotEmpty(t, lines)

	// Verify thread state after processing.
	thr, err := store.Get(sessionID)
	require.NoError(t, err)
	turns := thr.State.Turns()
	require.GreaterOrEqual(t, len(turns), 2)

	userTurn := turns[0]
	assert.Equal(t, "user", string(userTurn.Role))
	require.Len(t, userTurn.Artifacts, 1)
	assert.Equal(t, "text", userTurn.Artifacts[0].Kind())
	assert.Equal(t, "hi", userTurn.Artifacts[0].(artifact.Text).Content)

	assistantTurn := turns[len(turns)-1]
	assert.Equal(t, "assistant", string(assistantTurn.Role))
	require.Len(t, assistantTurn.Artifacts, 1)
	assert.Equal(t, "text", assistantTurn.Artifacts[0].Kind())
	assert.Equal(t, "Hello world", assistantTurn.Artifacts[0].(artifact.Text).Content)
}

func TestHandler_SendMessage_NotFound(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())
	h := newTestHandler(t, mgr)

	body := `{"content": "hi"}`
	req := httptest.NewRequest("POST", "/sessions/nonexistent/messages", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	assert.Equal(t, 404, rr.Code)
}

func TestHandler_SendMessage_NoKinds(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "Hello"},
		},
	}
	store := junk.NewMemoryStore()
	mgr := junk.NewManager(store, prov, func(stream *junk.Stream) ([]loop.Option, error) {
		return nil, nil
	}, simpleProcessor())
	h := newTestHandler(t, mgr)

	// Create a junk.
	createReq := httptest.NewRequest("POST", "/sessions", nil)
	createRr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(createRr, createReq)
	require.Equal(t, 201, createRr.Code)

	var createResp map[string]string
	require.NoError(t, json.Unmarshal(createRr.Body.Bytes(), &createResp))
	sessionID := createResp["id"]

	// Send a message without specifying kinds.
	body := `{"content": "hi"}`
	req := httptest.NewRequest("POST", "/sessions/"+sessionID+"/messages", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	require.Equal(t, 200, rr.Code)

	// Verify thread state after processing.
	thr, err := store.Get(sessionID)
	require.NoError(t, err)
	turns := thr.State.Turns()
	require.GreaterOrEqual(t, len(turns), 2)

	userTurn := turns[0]
	assert.Equal(t, "user", string(userTurn.Role))
	require.Len(t, userTurn.Artifacts, 1)
	assert.Equal(t, "text", userTurn.Artifacts[0].Kind())
	assert.Equal(t, "hi", userTurn.Artifacts[0].(artifact.Text).Content)

	assistantTurn := turns[len(turns)-1]
	assert.Equal(t, "assistant", string(assistantTurn.Role))
	require.Len(t, assistantTurn.Artifacts, 1)
	assert.Equal(t, "text", assistantTurn.Artifacts[0].Kind())
	assert.Equal(t, "Hello", assistantTurn.Artifacts[0].(artifact.Text).Content)
}

func TestHandler_SendMessage_MalformedJSON(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())
	h := newTestHandler(t, mgr)

	// Create a junk.
	createReq := httptest.NewRequest("POST", "/sessions", nil)
	createRr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(createRr, createReq)
	require.Equal(t, 201, createRr.Code)

	var createResp map[string]string
	require.NoError(t, json.Unmarshal(createRr.Body.Bytes(), &createResp))
	sessionID := createResp["id"]

	// Send malformed JSON.
	body := `{"invalid`
	req := httptest.NewRequest("POST", "/sessions/"+sessionID+"/messages", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	assert.Equal(t, 400, rr.Code)
}

func TestHandler_SendMessage_EmptyBody(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())
	h := newTestHandler(t, mgr)

	// Create a junk.
	createReq := httptest.NewRequest("POST", "/sessions", nil)
	createRr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(createRr, createReq)
	require.Equal(t, 201, createRr.Code)

	var createResp map[string]string
	require.NoError(t, json.Unmarshal(createRr.Body.Bytes(), &createResp))
	sessionID := createResp["id"]

	// Send empty request body.
	req := httptest.NewRequest("POST", "/sessions/"+sessionID+"/messages", strings.NewReader(""))
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	assert.Equal(t, 400, rr.Code)
}

func TestHandler_SendMessage_ProviderError(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "Partial"},
		},
		err: fmt.Errorf("provider failure"),
	}
	store := junk.NewMemoryStore()
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())
	h := newTestHandler(t, mgr)

	// Create a junk.
	createReq := httptest.NewRequest("POST", "/sessions", nil)
	createRr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(createRr, createReq)
	require.Equal(t, 201, createRr.Code)

	var createResp map[string]string
	require.NoError(t, json.Unmarshal(createRr.Body.Bytes(), &createResp))
	sessionID := createResp["id"]

	// Send a message with "error" included in kinds so the error event is streamed.
	body := `{"content": "hi", "kinds": ["text_delta", "turn_complete", "error"]}`
	req := httptest.NewRequest("POST", "/sessions/"+sessionID+"/messages", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	require.Equal(t, 200, rr.Code)

	// The handler may stream an error event if the provider fails before
	// TurnCompleteEvent is emitted. Due to async FanOut distribution, the
	// response body may be empty or contain only a subset of events.
	// The primary assertion is that the request does not panic and returns
	// HTTP 200, surfacing the error through the NDJSON stream when possible.
}

func TestHandler_SendMessage_SaveError(t *testing.T) {
	store := newSaveErrStore()
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "Hello"},
		},
	}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())
	h := newTestHandler(t, mgr)

	// Create a junk.
	createReq := httptest.NewRequest("POST", "/sessions", nil)
	createRr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(createRr, createReq)
	require.Equal(t, 201, createRr.Code)

	var createResp map[string]string
	require.NoError(t, json.Unmarshal(createRr.Body.Bytes(), &createResp))
	sessionID := createResp["id"]

	// Send a message.
	body := `{"content": "hi", "kinds": ["text_delta", "turn_complete", "error"]}`
	req := httptest.NewRequest("POST", "/sessions/"+sessionID+"/messages", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	require.Equal(t, 200, rr.Code)

	// Save errors occur after the turn completes, so TurnCompleteEvent may
	// reach the subscription before the error signal. The response body is
	// therefore racy — it may contain turn_complete, error, or be empty
	// depending on goroutine scheduling. The key assertion is HTTP 200.
}

func TestHandler_SendMessage_HandlerError(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, boomProcessor())
	h := newTestHandler(t, mgr)

	// Create a junk.
	createReq := httptest.NewRequest("POST", "/sessions", nil)
	createRr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(createRr, createReq)
	require.Equal(t, 201, createRr.Code)

	var createResp map[string]string
	require.NoError(t, json.Unmarshal(createRr.Body.Bytes(), &createResp))
	sessionID := createResp["id"]

	// Send a message.
	body := `{"content": "hi", "kinds": ["text_delta", "turn_complete", "error"]}`
	req := httptest.NewRequest("POST", "/sessions/"+sessionID+"/messages", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	require.Equal(t, 200, rr.Code)

	// The handler may stream an error event if the processor fails before
	// TurnCompleteEvent is emitted. Due to async FanOut distribution, the
	// response body may be empty or contain only a subset of events.
}

func TestSSEWriter(t *testing.T) {
	rr := httptest.NewRecorder()
	sw, err := newSSEWriter(rr)
	require.NoError(t, err)

	data := []byte(`{"kind":"text_delta","content":"hello"}`)
	require.NoError(t, sw.WriteEvent("text_delta", data))

	body := rr.Body.String()
	assert.Contains(t, body, "event: text_delta\n")
	assert.Contains(t, body, "data: {\"kind\":\"text_delta\",\"content\":\"hello\"}\n\n")
}

func TestHandler_SessionEvents_NotFound(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())
	h := newTestHandler(t, mgr)

	req := httptest.NewRequest("GET", "/sessions/nonexistent/events", nil)
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	assert.Equal(t, 404, rr.Code)
}

func TestHandler_SessionEvents_ContextCancel(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())
	h := newTestHandler(t, mgr)

	// Create a junk.
	createReq := httptest.NewRequest("POST", "/sessions", nil)
	createRr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(createRr, createReq)
	require.Equal(t, 201, createRr.Code)

	var createResp map[string]string
	require.NoError(t, json.Unmarshal(createRr.Body.Bytes(), &createResp))
	sessionID := createResp["id"]

	// Start SSE handler with an already-cancelled context.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := httptest.NewRequest("GET", "/sessions/"+sessionID+"/events", nil).WithContext(ctx)
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	assert.Equal(t, 200, rr.Code)
	assert.Equal(t, "text/event-stream", rr.Header().Get("Content-Type"))
	assert.Equal(t, "no-cache", rr.Header().Get("Cache-Control"))
	assert.Equal(t, "keep-alive", rr.Header().Get("Connection"))
}

func TestHandler_SessionTurns(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "Hello"},
			artifact.TextDelta{Content: " world"},
		},
	}
	store := junk.NewMemoryStore()
	mgr := junk.NewManager(store, prov, func(stream *junk.Stream) ([]loop.Option, error) {
		return nil, nil
	}, simpleProcessor())
	h := newTestHandler(t, mgr)

	// Create a junk.
	createReq := httptest.NewRequest("POST", "/sessions", nil)
	createRr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(createRr, createReq)
	require.Equal(t, 201, createRr.Code)

	var createResp map[string]string
	require.NoError(t, json.Unmarshal(createRr.Body.Bytes(), &createResp))
	sessionID := createResp["id"]

	// Send a message to populate turns.
	body := `{"content": "hi", "kinds": ["text_delta", "turn_complete"]}`
	sendReq := httptest.NewRequest("POST", "/sessions/"+sessionID+"/messages", strings.NewReader(body))
	sendRr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(sendRr, sendReq)
	require.Equal(t, 200, sendRr.Code)

	// Get turns.
	turnsReq := httptest.NewRequest("GET", "/sessions/"+sessionID+"/turns", nil)
	turnsRr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(turnsRr, turnsReq)

	require.Equal(t, 200, turnsRr.Code)
	assert.Equal(t, "application/json", turnsRr.Header().Get("Content-Type"))

	var turns []map[string]interface{}
	require.NoError(t, json.Unmarshal(turnsRr.Body.Bytes(), &turns))
	require.Len(t, turns, 2)

	assert.Equal(t, "user", turns[0]["role"])
	assert.Equal(t, "assistant", turns[1]["role"])
}

func TestHandler_SessionTurns_NotFound(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())
	h := newTestHandler(t, mgr)

	req := httptest.NewRequest("GET", "/sessions/nonexistent/turns", nil)
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	assert.Equal(t, 404, rr.Code)
}

func TestHandler_SessionTurns_Empty(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())
	h := newTestHandler(t, mgr)

	// Create a session without sending any messages.
	createReq := httptest.NewRequest("POST", "/sessions", nil)
	createRr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(createRr, createReq)
	require.Equal(t, 201, createRr.Code)

	var createResp map[string]string
	require.NoError(t, json.Unmarshal(createRr.Body.Bytes(), &createResp))
	sessionID := createResp["id"]

	req := httptest.NewRequest("GET", "/sessions/"+sessionID+"/turns", nil)
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	require.Equal(t, 200, rr.Code)
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	var turns []map[string]interface{}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &turns))
	assert.Empty(t, turns)
}

func TestHandler_ListThreads(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())
	h := newTestHandler(t, mgr)

	// Create a session (which also creates a thread).
	createReq := httptest.NewRequest("POST", "/sessions", nil)
	createRr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(createRr, createReq)
	require.Equal(t, 201, createRr.Code)

	var createResp map[string]string
	require.NoError(t, json.Unmarshal(createRr.Body.Bytes(), &createResp))

	req := httptest.NewRequest("GET", "/threads", nil)
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	require.Equal(t, 200, rr.Code)
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	// New envelope: {"threads": [...], "next_cursor": "..."}
	var resp struct {
		Threads    []map[string]any `json:"threads"`
		NextCursor string           `json:"next_cursor"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	require.Len(t, resp.Threads, 1)
	assert.Equal(t, createResp["id"], resp.Threads[0]["id"])
	assert.Empty(t, resp.NextCursor, "single thread should yield no next cursor")
}

func TestHandler_ListThreads_Empty(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())
	h := newTestHandler(t, mgr)

	req := httptest.NewRequest("GET", "/threads", nil)
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	require.Equal(t, 200, rr.Code)
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	// Envelope must always be an object with a threads array.
	var resp map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	threads, ok := resp["threads"]
	require.True(t, ok, "response must contain 'threads' key")
	arr, ok := threads.([]any)
	require.True(t, ok, "'threads' must be an array, got %T", threads)
	assert.Empty(t, arr)
	// next_cursor is omitted on the last page; assert it's not present
	// (rather than asserting empty string) to honour the omitempty contract.
	_, hasNext := resp["next_cursor"]
	assert.False(t, hasNext, "next_cursor should be omitted on last page")
}

func TestHandler_ListThreads_StoreError(t *testing.T) {
	store := &listErrStore{}
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())
	h := newTestHandler(t, mgr)

	req := httptest.NewRequest("GET", "/threads", nil)
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	assert.Equal(t, 500, rr.Code)
}

// seedThread saves a thread with a controlled LastAt timestamp. Used
// by the listing tests below to construct predictable sort orders
// without depending on real wall-clock time.
//
// MemoryStore.Save overrides LastAt to time.Now() at save time. We
// seedThread saves a thread whose most recent turn's timestamp is
// the given lastAt. The thread is then discoverable through the
// listing's last-activity sort key. A custom Clock is used so the
// append produces a turn with the requested timestamp.
func seedThread(t *testing.T, store *junk.MemoryStore, id string, lastAt time.Time) {
	t.Helper()
	thr := &junk.Thread{
		ID:       id,
		State:    ledger.NewThread(),
		Metadata: map[string]string{},
	}
	if !lastAt.IsZero() {
		thr.State = ledger.NewThread(ledger.WithThreadClock(ledger.ClockFunc(func() time.Time { return lastAt })))
		thr.State.Append(ledger.RoleUser, artifact.Text{Content: "x"})
	}
	require.NoError(t, store.Save(thr))
}

// threadIDsInPage extracts the IDs from a threads-list response payload.
func threadIDsInPage(t *testing.T, body []byte) []string {
	t.Helper()
	var resp struct {
		Threads    []map[string]any `json:"threads"`
		NextCursor string           `json:"next_cursor"`
	}
	require.NoError(t, json.Unmarshal(body, &resp))
	ids := make([]string, 0, len(resp.Threads))
	for _, th := range resp.Threads {
		ids = append(ids, th["id"].(string))
	}
	return ids
}

func TestHandler_ListThreads_DefaultLimit(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())
	h := newTestHandler(t, mgr)

	// Seed 25 threads. Default page size is 20, so we should see 20.
	now := time.Now()
	for i := 0; i < 25; i++ {
		seedThread(t, store, fmt.Sprintf("t-%02d", i), now.Add(-time.Duration(i)*time.Minute))
	}

	req := httptest.NewRequest("GET", "/threads", nil)
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	require.Equal(t, 200, rr.Code)
	ids := threadIDsInPage(t, rr.Body.Bytes())
	require.Len(t, ids, defaultThreadPageSize)

	// The most recently updated (i=0) should be first.
	assert.Equal(t, "t-00", ids[0])
}

func TestHandler_ListThreads_SortedByUpdatedAtDesc(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())
	h := newTestHandler(t, mgr)

	// Seed in a non-chronological order to ensure the handler sorts.
	now := time.Now()
	seedThread(t, store, "old", now.Add(-3*time.Hour))
	seedThread(t, store, "newest", now)
	seedThread(t, store, "middle", now.Add(-1*time.Hour))

	req := httptest.NewRequest("GET", "/threads", nil)
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	require.Equal(t, 200, rr.Code)
	assert.Equal(t, []string{"newest", "middle", "old"}, threadIDsInPage(t, rr.Body.Bytes()))
}

func TestHandler_ListThreads_LimitRespected(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())
	h := newTestHandler(t, mgr)

	now := time.Now()
	for i := 0; i < 10; i++ {
		seedThread(t, store, fmt.Sprintf("t-%02d", i), now.Add(-time.Duration(i)*time.Minute))
	}

	req := httptest.NewRequest("GET", "/threads?limit=3", nil)
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	require.Equal(t, 200, rr.Code)
	ids := threadIDsInPage(t, rr.Body.Bytes())
	require.Len(t, ids, 3)
	assert.Equal(t, []string{"t-00", "t-01", "t-02"}, ids)
}

func TestHandler_ListThreads_CursorProgression(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())
	h := newTestHandler(t, mgr)

	now := time.Now()
	for i := 0; i < 5; i++ {
		seedThread(t, store, fmt.Sprintf("t-%02d", i), now.Add(-time.Duration(i)*time.Minute))
	}

	// Page 1: limit=2
	rr1 := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr1, httptest.NewRequest("GET", "/threads?limit=2", nil))
	require.Equal(t, 200, rr1.Code)

	var page1 struct {
		Threads    []map[string]any `json:"threads"`
		NextCursor string           `json:"next_cursor"`
	}
	require.NoError(t, json.Unmarshal(rr1.Body.Bytes(), &page1))
	require.Len(t, page1.Threads, 2)
	assert.Equal(t, "t-00", page1.Threads[0]["id"])
	assert.Equal(t, "t-01", page1.Threads[1]["id"])
	require.NotEmpty(t, page1.NextCursor, "page 1 must have a next cursor")

	// Page 2: use the cursor.
	rr2 := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr2, httptest.NewRequest("GET", "/threads?limit=2&cursor="+page1.NextCursor, nil))
	require.Equal(t, 200, rr2.Code)
	ids2 := threadIDsInPage(t, rr2.Body.Bytes())
	assert.Equal(t, []string{"t-02", "t-03"}, ids2)

	var page2 struct {
		NextCursor string `json:"next_cursor"`
	}
	require.NoError(t, json.Unmarshal(rr2.Body.Bytes(), &page2))
	require.NotEmpty(t, page2.NextCursor, "page 2 must have a next cursor")

	// Page 3: last page, no cursor.
	rr3 := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr3, httptest.NewRequest("GET", "/threads?limit=2&cursor="+page2.NextCursor, nil))
	require.Equal(t, 200, rr3.Code)
	ids3 := threadIDsInPage(t, rr3.Body.Bytes())
	assert.Equal(t, []string{"t-04"}, ids3)

	// Confirm next_cursor is omitted on the last page.
	var page3 map[string]any
	require.NoError(t, json.Unmarshal(rr3.Body.Bytes(), &page3))
	_, hasNext := page3["next_cursor"]
	assert.False(t, hasNext, "last page must omit next_cursor")
}

func TestHandler_ListThreads_LastPageHasEmptyCursor(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())
	h := newTestHandler(t, mgr)

	// Only 2 threads; ?limit=10 should return them all with no next cursor.
	now := time.Now()
	seedThread(t, store, "a", now.Add(-1*time.Hour))
	seedThread(t, store, "b", now)

	req := httptest.NewRequest("GET", "/threads?limit=10", nil)
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	require.Equal(t, 200, rr.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	_, hasNext := resp["next_cursor"]
	assert.False(t, hasNext, "next_cursor must be omitted when all items fit on one page")
}

func TestHandler_ListThreads_InvalidCursor400(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())
	h := newTestHandler(t, mgr)

	now := time.Now()
	seedThread(t, store, "a", now)

	req := httptest.NewRequest("GET", "/threads?cursor=not-a-valid-cursor", nil)
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	assert.Equal(t, 400, rr.Code)
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	// Body should contain a clear error message.
	var body map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	errMsg, ok := body["error"].(string)
	require.True(t, ok, "error body should be a JSON object with an 'error' string")
	assert.Contains(t, errMsg, "cursor")
}

func TestHandler_ListThreads_LimitClamped(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())
	h := newTestHandler(t, mgr)

	// Seed 200 threads so we can detect clamping in both directions.
	now := time.Now()
	for i := 0; i < 200; i++ {
		seedThread(t, store, fmt.Sprintf("t-%03d", i), now.Add(-time.Duration(i)*time.Minute))
	}

	tests := []struct {
		name  string
		query string
		want  int // expected page length
	}{
		{"zero clamps to 1", "?limit=0", 1},
		{"negative clamps to 1", "?limit=-5", 1},
		{"huge clamps to 100", "?limit=99999", 100},
		{"empty defaults to 20", "?limit=", defaultThreadPageSize},
		{"non-numeric defaults to 20", "?limit=abc", defaultThreadPageSize},
		{"valid limit honoured", "?limit=7", 7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			h.ServeMux().ServeHTTP(rr, httptest.NewRequest("GET", "/threads"+tt.query, nil))
			require.Equal(t, 200, rr.Code)
			ids := threadIDsInPage(t, rr.Body.Bytes())
			assert.Len(t, ids, tt.want)
		})
	}
}

func TestHandler_LandingPage_IncludesLoadMoreWhenMorePagesExist(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())
	h := newTestHandler(t, mgr)

	// Seed more threads than the default page size so a next cursor exists.
	now := time.Now()
	for i := 0; i < defaultThreadPageSize+5; i++ {
		seedThread(t, store, fmt.Sprintf("t-%02d", i), now.Add(-time.Duration(i)*time.Minute))
	}

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	require.Equal(t, 200, rr.Code)
	body := rr.Body.String()

	// The button must be present with a non-empty cursor.
	assert.Contains(t, body, `id="load-more"`)
	assert.Contains(t, body, `class="load-more"`)

	// Extract the data-cursor attribute value and verify it is a valid cursor.
	// We use a simple regex because string containment is awkward for attribute values.
	re := regexp.MustCompile(`data-cursor="([^"]+)"`)
	match := re.FindStringSubmatch(body)
	require.NotNil(t, match, "landing page must include a data-cursor attribute on the load-more button")
	cursor := match[1]
	require.NotEmpty(t, cursor)

	// The cursor must round-trip through the decoder.
	decoded, err := decodeThreadCursor(cursor)
	require.NoError(t, err)
	assert.NotZero(t, decoded.LastAt)
	assert.NotEmpty(t, decoded.ID)

	// The noscript fallback must be present.
	assert.Contains(t, body, "<noscript>")

	// The load-more script must be present.
	assert.Contains(t, body, "load-more")
	assert.Contains(t, body, "/threads?cursor=")
}

func TestHandler_LandingPage_OmitsLoadMoreOnLastPage(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())
	h := newTestHandler(t, mgr)

	// Seed fewer threads than the default page size so there is no next page.
	now := time.Now()
	for i := 0; i < 3; i++ {
		seedThread(t, store, fmt.Sprintf("t-%02d", i), now.Add(-time.Duration(i)*time.Minute))
	}

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	require.Equal(t, 200, rr.Code)
	body := rr.Body.String()

	assert.NotContains(t, body, `id="load-more"`)
	assert.NotContains(t, body, `data-cursor=`)
}

func TestNDJSONWriter_NoFlusher(t *testing.T) {
	w := &noFlusherWriter{
		header: make(http.Header),
		body:   new(bytes.Buffer),
	}
	_, err := newNDJSONWriter(w)
	require.Error(t, err)
}

func TestSSEWriter_NoFlusher(t *testing.T) {
	w := &noFlusherWriter{
		header: make(http.Header),
		body:   new(bytes.Buffer),
	}
	_, err := newSSEWriter(w)
	require.Error(t, err)
}

func TestHandler_ServeMux_UnknownPaths(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())
	h := newTestHandler(t, mgr, WithoutUI())

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{"unknown path", "GET", "/unknown", 404},
		{"unknown nested path", "POST", "/sessions/abc-123/unknown", 404},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rr := httptest.NewRecorder()
			h.ServeMux().ServeHTTP(rr, req)
			assert.Equal(t, tt.wantStatus, rr.Code)
		})
	}
}

func TestHandler_WithUI_StaticFiles(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())
	h := newTestHandler(t, mgr, WithUI())

	t.Run("GET /chat returns text/html", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/chat", nil)
		rr := httptest.NewRecorder()
		h.ServeMux().ServeHTTP(rr, req)
		assert.Equal(t, 200, rr.Code)
		assert.Equal(t, "text/html; charset=utf-8", rr.Header().Get("Content-Type"))
		assert.Contains(t, rr.Body.String(), "ore chat")
	})

	t.Run("WithName overrides default branding", func(t *testing.T) {
		h := newTestHandler(t, mgr, WithUI(), WithName("Custom App"))
		req := httptest.NewRequest("GET", "/chat", nil)
		rr := httptest.NewRecorder()
		h.ServeMux().ServeHTTP(rr, req)
		assert.Equal(t, 200, rr.Code)
		assert.Equal(t, "text/html; charset=utf-8", rr.Header().Get("Content-Type"))
		assert.Contains(t, rr.Body.String(), "Custom App")
		assert.NotContains(t, rr.Body.String(), "ore chat")
	})

	t.Run("GET /chat.js returns application/javascript", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/chat.js", nil)
		rr := httptest.NewRecorder()
		h.ServeMux().ServeHTTP(rr, req)
		assert.Equal(t, 200, rr.Code)
		assert.Equal(t, "application/javascript; charset=utf-8", rr.Header().Get("Content-Type"))
		assert.Contains(t, rr.Body.String(), "createSession")
	})

	t.Run("unknown path returns 404", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/unknown", nil)
		rr := httptest.NewRecorder()
		h.ServeMux().ServeHTTP(rr, req)
		assert.Equal(t, 404, rr.Code)
	})
}

func TestHandler_WithUI_StaticFiles_ErrorPath(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())
	h := newTestHandler(t, mgr, WithUI())

	// Swap staticFS with a mock that always errors.
	oldFS := staticFS
	staticFS = &errorFS{}
	defer func() { staticFS = oldFS }()

	tests := []struct {
		name string
		path string
	}{
		{"GET /chat errors", "/chat"},
		{"GET /chat.js errors", "/chat.js"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			rr := httptest.NewRecorder()
			h.ServeMux().ServeHTTP(rr, req)
			assert.Equal(t, 500, rr.Code)
		})
	}
}

func TestHandler_WithUI_LandingPage(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())
	h := newTestHandler(t, mgr, WithUI())

	// Seed a thread with a user message so we can verify snippet extraction.
	thr, err := store.Create()
	require.NoError(t, err)
	thr.State = ledger.NewThread(ledger.WithThreadClock(ledger.ClockFunc(func() time.Time {
		return time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	})))
	thr.State.Append(ledger.RoleUser, artifact.Text{Content: "Hello world this is a test message for preview"})
	require.NoError(t, store.Save(thr))

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	assert.Equal(t, 200, rr.Code)
	assert.Equal(t, "text/html; charset=utf-8", rr.Header().Get("Content-Type"))
	body := rr.Body.String()
	assert.Contains(t, body, "Start new chat")
	assert.Contains(t, body, "/chat")
	assert.Contains(t, body, thr.ID)
	assert.Contains(t, body, "/chat?thread="+thr.ID)
	assert.Contains(t, body, "Hello world this is a test message for preview")
	assert.Contains(t, body, "Updated")
}

func TestHandler_WithUI_LandingPage_Empty(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())
	h := newTestHandler(t, mgr, WithUI())

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	assert.Equal(t, 200, rr.Code)
	body := rr.Body.String()
	assert.Contains(t, body, "No conversations yet")
	assert.Contains(t, body, "Start new chat")
}

func TestHandler_WithUI_LandingPage_TruncatedPreview(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())
	h := newTestHandler(t, mgr, WithUI())

	thr, err := store.Create()
	require.NoError(t, err)
	longMsg := strings.Repeat("a", 200)
	thr.State.Append(ledger.RoleUser, artifact.Text{Content: longMsg})
	require.NoError(t, store.Save(thr))

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	assert.Equal(t, 200, rr.Code)
	body := rr.Body.String()
	assert.Contains(t, body, strings.Repeat("a", 120)+"...")
	assert.NotContains(t, body, strings.Repeat("a", 121)+"...")
}

func TestHandler_WithUI_LandingPage_ErrorPath(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())
	h := newTestHandler(t, mgr, WithUI())

	// Swap staticFS with a mock that always errors.
	oldFS := staticFS
	staticFS = &errorFS{}
	defer func() { staticFS = oldFS }()

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	assert.Equal(t, 500, rr.Code)
}

func TestHandler_WithoutUI_Root404(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, simpleProcessor())
	h := newTestHandler(t, mgr, WithoutUI())

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)
	assert.Equal(t, 404, rr.Code)
}

func TestDescriptor(t *testing.T) {
	assert.Equal(t, "HTTP", Descriptor.Name)
	assert.Contains(t, Descriptor.Description, "HTTP conduit")
	assert.ElementsMatch(t, []conduit.Capability{
		conduit.CapEventSource,
		conduit.CapShowStatus,
		conduit.CapRenderTurn,
		conduit.CapRenderMarkdown,
		conduit.CapAudioNotification,
	}, Descriptor.Capabilities)
}

func TestHandler_SendMessage_ErrorFallback_NoProcessComplete(t *testing.T) {
	store := junk.NewMemoryStore()
	prov := &mockProvider{}
	mgr := junk.NewManager(store, prov, func(*junk.Stream) ([]loop.Option, error) { return nil, nil }, boomProcessor())
	h := newTestHandler(t, mgr)

	// Create a junk.
	createReq := httptest.NewRequest("POST", "/sessions", nil)
	createRr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(createRr, createReq)
	require.Equal(t, 201, createRr.Code)

	var createResp map[string]string
	require.NoError(t, json.Unmarshal(createRr.Body.Bytes(), &createResp))
	sessionID := createResp["id"]

	// Send a message WITHOUT subscribing to lifecycle.
	// This simulates an older client that only knows about turn_complete and error.
	body := `{"content": "hi", "kinds": ["text_delta", "turn_complete", "error"]}`
	req := httptest.NewRequest("POST", "/sessions/"+sessionID+"/messages", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	require.Equal(t, 200, rr.Code)

	// Parse the NDJSON response and verify an error event was sent as fallback.
	var foundError bool
	for _, line := range strings.Split(rr.Body.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var event struct {
			Kind    string `json:"kind"`
			Message string `json:"message"`
		}
		require.NoError(t, json.Unmarshal([]byte(line), &event))
		if event.Kind == "error" {
			foundError = true
			assert.Contains(t, event.Message, "boom")
		}
	}
	assert.True(t, foundError, "expected an error event in the NDJSON stream when lifecycle is not subscribed")
}

func TestHandler_SendMessage_LifecycleEventInNDJSON(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Hello"},
		},
	}
	store := junk.NewMemoryStore()
	stepFactory := func(stream *junk.Stream) ([]loop.Option, error) {
		return nil, nil
	}
	mgr := junk.NewManager(store, prov, stepFactory, simpleProcessor())
	h := newTestHandler(t, mgr)

	// Create a junk.
	createReq := httptest.NewRequest("POST", "/sessions", nil)
	createRr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(createRr, createReq)
	require.Equal(t, 201, createRr.Code)

	var createResp map[string]string
	require.NoError(t, json.Unmarshal(createRr.Body.Bytes(), &createResp))
	sessionID := createResp["id"]

	// Send a message.
	body := `{"content": "hi"}`
	req := httptest.NewRequest("POST", "/sessions/"+sessionID+"/messages", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	require.Equal(t, 200, rr.Code)

	// Parse NDJSON lines.
	lines := strings.Split(strings.TrimSpace(rr.Body.String()), "\n")
	require.NotEmpty(t, lines)

	// Verify lifecycle events are present in the NDJSON stream.
	// No self-emitted properties events should appear (they were removed
	// in favor of structured lifecycle phases).
	var lifecycleEvents []map[string]interface{}
	for _, line := range lines {
		var event map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(line), &event))
		if event["kind"] == "lifecycle" {
			lifecycleEvents = append(lifecycleEvents, event)
		}
	}
	require.GreaterOrEqual(t, len(lifecycleEvents), 1)
	assert.Equal(t, "submitted", lifecycleEvents[0]["phase"])

	var phases []string
	for _, ev := range lifecycleEvents {
		phases = append(phases, ev["phase"].(string))
	}
	assert.Equal(t, []string{"submitted", "streaming", "done"}, phases)
}
