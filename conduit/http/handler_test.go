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

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/conversation"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/state"
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

// noFlusherWriter implements http.ResponseWriter but NOT http.Flusher.
// Used to test writer creation failure paths.
type noFlusherWriter struct {
	header http.Header
	body   *bytes.Buffer
	code   int
}

func (w *noFlusherWriter) Header() http.Header {
	return w.header
}

func (w *noFlusherWriter) Write(b []byte) (int, error) {
	return w.body.Write(b)
}

func (w *noFlusherWriter) WriteHeader(code int) {
	w.code = code
}

// errorFS is a test double for fs.ReadFileFS that always returns an error.
type errorFS struct{}

func (e *errorFS) Open(name string) (fs.File, error) {
	return nil, fs.ErrNotExist
}

func (e *errorFS) ReadFile(name string) ([]byte, error) {
	return nil, fs.ErrNotExist
}

// simpleMessageHandler returns a MessageHandler that only submits the user
// message as a non-inference turn. It does not invoke the provider.
func simpleMessageHandler() MessageHandler {
	return func(ctx context.Context, session *Session, content string) error {
		_, err := session.Step().Submit(ctx, session.State(), state.RoleUser, artifact.Text{Content: content})
		return err
	}
}

// turnMessageHandler returns a MessageHandler that submits the user message
// and then runs a single Step.Turn with the given provider.
func turnMessageHandler(p provider.Provider) MessageHandler {
	return func(ctx context.Context, session *Session, content string) error {
		if _, err := session.Step().Submit(ctx, session.State(), state.RoleUser, artifact.Text{Content: content}); err != nil {
			return err
		}
		_, err := session.Step().Turn(ctx, session.State(), p)
		return err
	}
}

// errStore is a Store that always returns an error from Create.
type errStore struct{}

func (e *errStore) Create() (*conversation.Conversation, error) {
	return nil, fmt.Errorf("store error")
}
func (e *errStore) Get(string) (*conversation.Conversation, bool)  { return nil, false }
func (e *errStore) Save(*conversation.Conversation) error           { return nil }
func (e *errStore) Delete(string) bool                               { return false }
func (e *errStore) List() ([]*conversation.Conversation, error)       { return nil, nil }

func TestNewHandler(t *testing.T) {
	store := conversation.NewMemoryStore()
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(store, newStep, simpleMessageHandler())
	require.NotNil(t, h)
	assert.NotNil(t, h.newStep)
	assert.NotNil(t, h.messageHandler)
	assert.NotNil(t, h.convStore)
	assert.NotNil(t, h.sessions)
}

func TestHandler_ServeMux_Routing(t *testing.T) {
	store := conversation.NewMemoryStore()
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(store, newStep, simpleMessageHandler())
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
		{"list conversations", "GET", "/conversations", 200},
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
	store := conversation.NewMemoryStore()
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(store, newStep, simpleMessageHandler())

	req := httptest.NewRequest("POST", "/sessions", nil)
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	require.Equal(t, 201, rr.Code)
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	var resp map[string]string
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	require.NotEmpty(t, resp["id"])
	assert.Equal(t, "/sessions/"+resp["id"]+"/events", resp["events_url"])

	// Verify the conversation exists in the store.
	_, ok := store.Get(resp["id"])
	assert.True(t, ok)
}

func TestHandler_CreateSession_StoresStep(t *testing.T) {
	store := conversation.NewMemoryStore()
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(store, newStep, simpleMessageHandler())

	req := httptest.NewRequest("POST", "/sessions", nil)
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	require.Equal(t, 201, rr.Code)

	var resp map[string]string
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))

	session, ok := h.sessions[resp["id"]]
	require.True(t, ok)
	assert.NotNil(t, session.step)
	assert.NotNil(t, session.conv)
}

func TestHandler_CreateSession_StoreError(t *testing.T) {
	store := &errStore{}
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(store, newStep, simpleMessageHandler())

	req := httptest.NewRequest("POST", "/sessions", nil)
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	assert.Equal(t, 500, rr.Code)
}

func TestHandler_CreateSession_AttachExisting(t *testing.T) {
	store := conversation.NewMemoryStore()
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(store, newStep, simpleMessageHandler())

	// Create a conversation directly in the store.
	conv, err := store.Create()
	require.NoError(t, err)

	// Attach to the existing conversation.
	body := fmt.Sprintf(`{"conversation_id": "%s"}`, conv.ID)
	req := httptest.NewRequest("POST", "/sessions", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	require.Equal(t, 201, rr.Code)

	var resp map[string]string
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Equal(t, conv.ID, resp["id"])

	// Verify the session references the same conversation.
	session, ok := h.sessions[conv.ID]
	require.True(t, ok)
	assert.Equal(t, conv.ID, session.conv.ID)
}

func TestHandler_CreateSession_AttachNotFound(t *testing.T) {
	store := conversation.NewMemoryStore()
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(store, newStep, simpleMessageHandler())

	body := `{"conversation_id": "nonexistent"}`
	req := httptest.NewRequest("POST", "/sessions", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	assert.Equal(t, 404, rr.Code)
}

func TestHandler_DeleteSession(t *testing.T) {
	store := conversation.NewMemoryStore()
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(store, newStep, simpleMessageHandler())

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

	// Verify the session no longer exists in the handler.
	_, ok := h.sessions[sessionID]
	assert.False(t, ok)

	// Verify the conversation still exists in the store.
	_, ok = store.Get(sessionID)
	assert.True(t, ok)
}

func TestHandler_DeleteSession_NotFound(t *testing.T) {
	store := conversation.NewMemoryStore()
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(store, newStep, simpleMessageHandler())

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
	store := conversation.NewMemoryStore()
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(store, newStep, turnMessageHandler(prov))

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
	store := conversation.NewMemoryStore()
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(store, newStep, simpleMessageHandler())

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
	store := conversation.NewMemoryStore()
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(store, newStep, turnMessageHandler(prov))

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
	store := conversation.NewMemoryStore()
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(store, newStep, simpleMessageHandler())

	req := httptest.NewRequest("GET", "/sessions/nonexistent/events", nil)
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	assert.Equal(t, 404, rr.Code)
}

func TestHandler_SessionEvents_ContextCancel(t *testing.T) {
	store := conversation.NewMemoryStore()
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(store, newStep, simpleMessageHandler())

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

func TestHandler_SendMessage_Concurrent(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "Hello"},
		},
	}
	store := conversation.NewMemoryStore()
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(store, newStep, turnMessageHandler(prov))

	// Create a session.
	createReq := httptest.NewRequest("POST", "/sessions", nil)
	createRr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(createRr, createReq)
	require.Equal(t, 201, createRr.Code)

	var createResp map[string]string
	require.NoError(t, json.Unmarshal(createRr.Body.Bytes(), &createResp))
	sessionID := createResp["id"]

	// Lock the session manually to simulate an in-progress turn.
	session := h.sessions[sessionID]
	require.True(t, session.Lock())

	// Send a message while the session is busy.
	body := `{"content": "hi", "kinds": ["text_delta", "turn_complete"]}`
	req := httptest.NewRequest("POST", "/sessions/"+sessionID+"/messages", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	assert.Equal(t, 409, rr.Code)

	// Unlock and clean up.
	session.Unlock()
}

func TestHandler_SendMessage_MalformedJSON(t *testing.T) {
	store := conversation.NewMemoryStore()
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(store, newStep, simpleMessageHandler())

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

func TestHandler_SendMessage_ProviderError(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "Partial"},
		},
		err: fmt.Errorf("provider failure"),
	}
	store := conversation.NewMemoryStore()
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(store, newStep, turnMessageHandler(prov))

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

func TestHandler_ListConversations(t *testing.T) {
	store := conversation.NewMemoryStore()
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(store, newStep, simpleMessageHandler())

	// Create a session (which also creates a conversation).
	createReq := httptest.NewRequest("POST", "/sessions", nil)
	createRr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(createRr, createReq)
	require.Equal(t, 201, createRr.Code)

	var createResp map[string]string
	require.NoError(t, json.Unmarshal(createRr.Body.Bytes(), &createResp))

	req := httptest.NewRequest("GET", "/conversations", nil)
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	require.Equal(t, 200, rr.Code)
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	var resp []map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	require.Len(t, resp, 1)
	assert.Equal(t, createResp["id"], resp[0]["id"])
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
	store := conversation.NewMemoryStore()
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(store, newStep, simpleMessageHandler())

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
	store := conversation.NewMemoryStore()
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(store, newStep, simpleMessageHandler(), WithUI())

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
	store := conversation.NewMemoryStore()
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(store, newStep, simpleMessageHandler(), WithUI())

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
	store := conversation.NewMemoryStore()
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(store, newStep, simpleMessageHandler())

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)
	assert.Equal(t, 404, rr.Code)
// boomMessageHandler returns a MessageHandler that always fails.
func boomMessageHandler() MessageHandler {
	return func(ctx context.Context, session *Session, content string) error {
		return fmt.Errorf("handler boom")
	}
}

// saveErrStore is a Store whose Save always returns an error.
// All other methods delegate to a real MemoryStore.
type saveErrStore struct {
	inner conversation.Store
}

func newSaveErrStore() *saveErrStore {
	return &saveErrStore{inner: conversation.NewMemoryStore()}
}

func (s *saveErrStore) Create() (*conversation.Conversation, error) {
	return s.inner.Create()
}
func (s *saveErrStore) Get(id string) (*conversation.Conversation, bool) {
	return s.inner.Get(id)
}
func (s *saveErrStore) Save(conv *conversation.Conversation) error {
	return fmt.Errorf("save failed")
}
func (s *saveErrStore) Delete(id string) bool {
	return s.inner.Delete(id)
}
func (s *saveErrStore) List() ([]*conversation.Conversation, error) {
	return s.inner.List()
}

// listErrStore is a Store whose List always returns an error.
type listErrStore struct{}

func (s *listErrStore) Create() (*conversation.Conversation, error) { return nil, nil }
func (s *listErrStore) Get(string) (*conversation.Conversation, bool) { return nil, false }
func (s *listErrStore) Save(*conversation.Conversation) error       { return nil }
func (s *listErrStore) Delete(string) bool                            { return false }
func (s *listErrStore) List() ([]*conversation.Conversation, error) {
	return nil, fmt.Errorf("list failed")
}

func TestHandler_SendMessage_SaveError(t *testing.T) {
	store := newSaveErrStore()
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "Hello"},
		},
	}
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(store, newStep, turnMessageHandler(prov))

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
	store := conversation.NewMemoryStore()
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(store, newStep, boomMessageHandler())

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
	assert.Contains(t, errorMsg, "handler boom")

	// Last line should still be the complete event.
	var complete completeEventJSON
	require.NoError(t, json.Unmarshal([]byte(lines[len(lines)-1]), &complete))
	assert.Equal(t, "complete", complete.Kind)
}

func TestHandler_ListConversations_StoreError(t *testing.T) {
	store := &listErrStore{}
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(store, newStep, simpleMessageHandler())

	req := httptest.NewRequest("GET", "/conversations", nil)
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	assert.Equal(t, 500, rr.Code)
}

func TestHandler_CreateSession_MalformedJSON(t *testing.T) {
	store := conversation.NewMemoryStore()
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(store, newStep, simpleMessageHandler())

	body := `{"conversation_id": "`
	req := httptest.NewRequest("POST", "/sessions", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	assert.Equal(t, 400, rr.Code)
}
