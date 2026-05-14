package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/conduit"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/state"
	"github.com/andrewhowdencom/ore/thread"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockProvider is a provider.Provider implementation for testing that can be
// configured to emit a sequence of artifacts, optionally returning an error.
type mockProvider struct {
	artifacts []artifact.Artifact
	err       error
}

func (m *mockProvider) Invoke(ctx context.Context, s state.State, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
	for _, art := range m.artifacts {
		select {
		case ch <- art:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return m.err
}

// blockingProvider is a provider that blocks until the context is cancelled.
type blockingProvider struct{}

func (m *blockingProvider) Invoke(ctx context.Context, s state.State, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
	<-ctx.Done()
	return ctx.Err()
}

// noFlusherWriter implements http.ResponseWriter but NOT http.Flusher.
// Used to test writer creation failure paths.
type noFlusherWriter struct {
	header http.Header
	body   *bytes.Buffer
	code   int
}

func (w *noFlusherWriter) Header() http.Header              { return w.header }
func (w *noFlusherWriter) Write(b []byte) (int, error)       { return w.body.Write(b) }
func (w *noFlusherWriter) WriteHeader(code int)              { w.code = code }

// errorFS is a test double for fs.ReadFileFS that always returns an error.
type errorFS struct{}

func (e *errorFS) Open(name string) (fs.File, error)       { return nil, fs.ErrNotExist }
func (e *errorFS) ReadFile(name string) ([]byte, error)   { return nil, fs.ErrNotExist }

// simpleProcessor runs a single Step.Turn with the mock provider.
func simpleProcessor() session.TurnProcessor {
	return func(ctx context.Context, step *loop.Step, st state.State, prov provider.Provider) (state.State, error) {
		return step.Turn(ctx, st, prov)
	}
}

// boomProcessor always fails.
func boomProcessor() session.TurnProcessor {
	return func(ctx context.Context, step *loop.Step, st state.State, prov provider.Provider) (state.State, error) {
		return st, fmt.Errorf("boom")
	}
}

// errStore is a Store that always returns an error from Create.
type errStore struct{}

func (e *errStore) Create() (*thread.Thread, error)                { return nil, fmt.Errorf("store error") }
func (e *errStore) Get(string) (*thread.Thread, bool)               { return nil, false }
func (e *errStore) Save(*thread.Thread) error                        { return nil }
func (e *errStore) Delete(string) bool                              { return false }
func (e *errStore) List() ([]*thread.Thread, error)                  { return nil, nil }

// saveErrStore is a Store whose Save always returns an error.
type saveErrStore struct {
	inner thread.Store
}

func newSaveErrStore() *saveErrStore {
	return &saveErrStore{inner: thread.NewMemoryStore()}
}

func (s *saveErrStore) Create() (*thread.Thread, error)            { return s.inner.Create() }
func (s *saveErrStore) Get(id string) (*thread.Thread, bool)       { return s.inner.Get(id) }
func (s *saveErrStore) Save(*thread.Thread) error                   { return fmt.Errorf("save failed") }
func (s *saveErrStore) Delete(string) bool                          { return s.inner.Delete("") }
func (s *saveErrStore) List() ([]*thread.Thread, error)             { return s.inner.List() }

// listErrStore is a Store whose List always returns an error.
type listErrStore struct{}

func (s *listErrStore) Create() (*thread.Thread, error)            { return nil, nil }
func (s *listErrStore) Get(string) (*thread.Thread, bool)           { return nil, false }
func (s *listErrStore) Save(*thread.Thread) error                    { return nil }
func (s *listErrStore) Delete(string) bool                          { return false }
func (s *listErrStore) List() ([]*thread.Thread, error)             { return nil, fmt.Errorf("list failed") }

func TestNewHandler(t *testing.T) {
	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := session.NewManager(store, prov, func() *loop.Step { return loop.New() }, simpleProcessor())
	h := NewHandler(mgr)
	require.NotNil(t, h)
	require.NotNil(t, h.mgr)
}

func TestHandler_ServeMux_Routing(t *testing.T) {
	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := session.NewManager(store, prov, func() *loop.Step { return loop.New() }, simpleProcessor())
	h := NewHandler(mgr)
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
	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := session.NewManager(store, prov, func() *loop.Step { return loop.New() }, simpleProcessor())
	h := NewHandler(mgr)

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
	_, ok := store.Get(resp["id"])
	assert.True(t, ok)
}

func TestHandler_CreateSession_StoreError(t *testing.T) {
	store := &errStore{}
	prov := &mockProvider{}
	mgr := session.NewManager(store, prov, func() *loop.Step { return loop.New() }, simpleProcessor())
	h := NewHandler(mgr)

	req := httptest.NewRequest("POST", "/sessions", nil)
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	assert.Equal(t, 500, rr.Code)
}

func TestHandler_CreateSession_AttachExisting(t *testing.T) {
	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := session.NewManager(store, prov, func() *loop.Step { return loop.New() }, simpleProcessor())
	h := NewHandler(mgr)

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
	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := session.NewManager(store, prov, func() *loop.Step { return loop.New() }, simpleProcessor())
	h := NewHandler(mgr)

	body := `{"thread_id": "nonexistent"}`
	req := httptest.NewRequest("POST", "/sessions", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	assert.Equal(t, 404, rr.Code)
}

func TestHandler_CreateSession_MalformedJSON(t *testing.T) {
	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := session.NewManager(store, prov, func() *loop.Step { return loop.New() }, simpleProcessor())
	h := NewHandler(mgr)

	body := `{"thread_id": "`
	req := httptest.NewRequest("POST", "/sessions", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	assert.Equal(t, 400, rr.Code)
}

func TestHandler_DeleteSession(t *testing.T) {
	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := session.NewManager(store, prov, func() *loop.Step { return loop.New() }, simpleProcessor())
	h := NewHandler(mgr)

	// Create a session first.
	createReq := httptest.NewRequest("POST", "/sessions", nil)
	createRr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(createRr, createReq)
	require.Equal(t, 201, createRr.Code)

	var resp map[string]string
	require.NoError(t, json.Unmarshal(createRr.Body.Bytes(), &resp))
	sessionID := resp["id"]

	// Delete the session.
	deleteReq := httptest.NewRequest("DELETE", "/sessions/"+sessionID, nil)
	deleteRr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(deleteRr, deleteReq)

	assert.Equal(t, 204, deleteRr.Code)
	assert.Empty(t, deleteRr.Body.String())

	// Verify the session is closed (subscribe should fail).
	_, err := mgr.Subscribe(sessionID, "text_delta")
	require.Error(t, err)

	// Verify the thread still exists in the store.
	_, ok := store.Get(sessionID)
	assert.True(t, ok)
}

func TestHandler_DeleteSession_NotFound(t *testing.T) {
	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := session.NewManager(store, prov, func() *loop.Step { return loop.New() }, simpleProcessor())
	h := NewHandler(mgr)

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
	store := thread.NewMemoryStore()
	mgr := session.NewManager(store, prov, func() *loop.Step { return loop.New() }, simpleProcessor())
	h := NewHandler(mgr)

	// Create a session.
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

	// Last line should be the complete event.
	var complete completeEventJSON
	require.NoError(t, json.Unmarshal([]byte(lines[len(lines)-1]), &complete))
	assert.Equal(t, "complete", complete.Kind)
	assert.GreaterOrEqual(t, len(complete.Turns), 2) // user + assistant

	// Verify user turn content.
	userTurn := complete.Turns[0]
	assert.Equal(t, "user", userTurn.Role)
	require.Len(t, userTurn.Artifacts, 1)
	assert.Equal(t, "text", userTurn.Artifacts[0].Kind)
	assert.Equal(t, "hi", userTurn.Artifacts[0].Content)

	// Verify assistant turn content.
	assistantTurn := complete.Turns[len(complete.Turns)-1]
	assert.Equal(t, "assistant", assistantTurn.Role)
	require.Len(t, assistantTurn.Artifacts, 1)
	assert.Equal(t, "text", assistantTurn.Artifacts[0].Kind)
	assert.Equal(t, "Hello world", assistantTurn.Artifacts[0].Content)
}

func TestHandler_SendMessage_NotFound(t *testing.T) {
	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := session.NewManager(store, prov, func() *loop.Step { return loop.New() }, simpleProcessor())
	h := NewHandler(mgr)

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
	store := thread.NewMemoryStore()
	mgr := session.NewManager(store, prov, func() *loop.Step { return loop.New() }, simpleProcessor())
	h := NewHandler(mgr)

	// Create a session.
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

	// Should still get the complete event at the end.
	lines := strings.Split(strings.TrimSpace(rr.Body.String()), "\n")
	require.NotEmpty(t, lines)

	var complete completeEventJSON
	require.NoError(t, json.Unmarshal([]byte(lines[len(lines)-1]), &complete))
	assert.Equal(t, "complete", complete.Kind)
}

func TestHandler_SendMessage_Concurrent(t *testing.T) {
	store := thread.NewMemoryStore()
	mgr := session.NewManager(store, &blockingProvider{}, func() *loop.Step { return loop.New() }, simpleProcessor())
	h := NewHandler(mgr)

	// Create a session.
	createReq := httptest.NewRequest("POST", "/sessions", nil)
	createRr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(createRr, createReq)
	require.Equal(t, 201, createRr.Code)

	var createResp map[string]string
	require.NoError(t, json.Unmarshal(createRr.Body.Bytes(), &createResp))
	sessionID := createResp["id"]

	// Lock the session by starting a blocking turn in a goroutine.
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		_ = mgr.Process(ctx, sessionID, conduit.UserMessageEvent{Content: "block"})
	}()

	// Wait briefly for the goroutine to acquire the lock.
	time.Sleep(50 * time.Millisecond)

	// Send a message while the session is busy.
	body := `{"content": "hi", "kinds": ["text_delta", "turn_complete"]}`
	req := httptest.NewRequest("POST", "/sessions/"+sessionID+"/messages", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	assert.Equal(t, 409, rr.Code)

	// Unlock and clean up.
	cancel()
}

func TestHandler_SendMessage_MalformedJSON(t *testing.T) {
	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := session.NewManager(store, prov, func() *loop.Step { return loop.New() }, simpleProcessor())
	h := NewHandler(mgr)

	// Create a session.
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
	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := session.NewManager(store, prov, func() *loop.Step { return loop.New() }, simpleProcessor())
	h := NewHandler(mgr)

	// Create a session.
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
	store := thread.NewMemoryStore()
	mgr := session.NewManager(store, prov, func() *loop.Step { return loop.New() }, simpleProcessor())
	h := NewHandler(mgr)

	// Create a session.
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

	// Parse NDJSON lines.
	lines := strings.Split(strings.TrimSpace(rr.Body.String()), "\n")
	require.NotEmpty(t, lines)

	// Check that at least one error event was streamed.
	var foundError bool
	for _, line := range lines {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err == nil {
			if event["kind"] == "error" {
				foundError = true
				break
			}
		}
	}
	assert.True(t, foundError, "expected an error event in the NDJSON stream")

	// Last line should still be the complete event.
	var complete completeEventJSON
	require.NoError(t, json.Unmarshal([]byte(lines[len(lines)-1]), &complete))
	assert.Equal(t, "complete", complete.Kind)
}

func TestHandler_SendMessage_SaveError(t *testing.T) {
	store := newSaveErrStore()
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "Hello"},
		},
	}
	mgr := session.NewManager(store, prov, func() *loop.Step { return loop.New() }, simpleProcessor())
	h := NewHandler(mgr)

	// Create a session.
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

	// Parse NDJSON lines.
	lines := strings.Split(strings.TrimSpace(rr.Body.String()), "\n")
	require.NotEmpty(t, lines)

	// Should contain an error event for the save failure.
	var foundError bool
	for _, line := range lines {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err == nil {
			if event["kind"] == "error" {
				foundError = true
				break
			}
		}
	}
	assert.True(t, foundError, "expected an error event for save failure")

	// Last line should still be the complete event.
	var complete completeEventJSON
	require.NoError(t, json.Unmarshal([]byte(lines[len(lines)-1]), &complete))
	assert.Equal(t, "complete", complete.Kind)
}

func TestHandler_SendMessage_HandlerError(t *testing.T) {
	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := session.NewManager(store, prov, func() *loop.Step { return loop.New() }, boomProcessor())
	h := NewHandler(mgr)

	// Create a session.
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

	// Parse NDJSON lines.
	lines := strings.Split(strings.TrimSpace(rr.Body.String()), "\n")
	require.NotEmpty(t, lines)

	// Should contain an error event for the handler failure.
	var foundError bool
	var errorMsg string
	for _, line := range lines {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err == nil {
			if event["kind"] == "error" {
				foundError = true
				if m, ok := event["message"].(string); ok {
					errorMsg = m
				}
				break
			}
		}
	}
	assert.True(t, foundError, "expected an error event for handler failure")
	assert.Contains(t, errorMsg, "boom")

	// Last line should still be the complete event.
	var complete completeEventJSON
	require.NoError(t, json.Unmarshal([]byte(lines[len(lines)-1]), &complete))
	assert.Equal(t, "complete", complete.Kind)
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
	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := session.NewManager(store, prov, func() *loop.Step { return loop.New() }, simpleProcessor())
	h := NewHandler(mgr)

	req := httptest.NewRequest("GET", "/sessions/nonexistent/events", nil)
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	assert.Equal(t, 404, rr.Code)
}

func TestHandler_SessionEvents_ContextCancel(t *testing.T) {
	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := session.NewManager(store, prov, func() *loop.Step { return loop.New() }, simpleProcessor())
	h := NewHandler(mgr)

	// Create a session.
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

func TestHandler_ListThreads(t *testing.T) {
	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := session.NewManager(store, prov, func() *loop.Step { return loop.New() }, simpleProcessor())
	h := NewHandler(mgr)

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

	var resp []map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	require.Len(t, resp, 1)
	assert.Equal(t, createResp["id"], resp[0]["id"])
}

func TestHandler_ListThreads_Empty(t *testing.T) {
	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := session.NewManager(store, prov, func() *loop.Step { return loop.New() }, simpleProcessor())
	h := NewHandler(mgr)

	req := httptest.NewRequest("GET", "/threads", nil)
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	require.Equal(t, 200, rr.Code)
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	var resp []map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Empty(t, resp)
}

func TestHandler_ListThreads_StoreError(t *testing.T) {
	store := &listErrStore{}
	prov := &mockProvider{}
	mgr := session.NewManager(store, prov, func() *loop.Step { return loop.New() }, simpleProcessor())
	h := NewHandler(mgr)

	req := httptest.NewRequest("GET", "/threads", nil)
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	assert.Equal(t, 500, rr.Code)
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
	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := session.NewManager(store, prov, func() *loop.Step { return loop.New() }, simpleProcessor())
	h := NewHandler(mgr)

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
	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := session.NewManager(store, prov, func() *loop.Step { return loop.New() }, simpleProcessor())
	h := NewHandler(mgr, WithUI())

	t.Run("GET / returns text/html", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		rr := httptest.NewRecorder()
		h.ServeMux().ServeHTTP(rr, req)
		assert.Equal(t, 200, rr.Code)
		assert.Equal(t, "text/html; charset=utf-8", rr.Header().Get("Content-Type"))
		assert.Contains(t, rr.Body.String(), "ore chat")
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
	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := session.NewManager(store, prov, func() *loop.Step { return loop.New() }, simpleProcessor())
	h := NewHandler(mgr, WithUI())

	// Swap staticFS with a mock that always errors.
	oldFS := staticFS
	staticFS = &errorFS{}
	defer func() { staticFS = oldFS }()

	tests := []struct {
		name string
		path string
	}{
		{"GET / errors", "/"},
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

func TestHandler_WithoutUI_Root404(t *testing.T) {
	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := session.NewManager(store, prov, func() *loop.Step { return loop.New() }, simpleProcessor())
	h := NewHandler(mgr)

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)
	assert.Equal(t, 404, rr.Code)
}
