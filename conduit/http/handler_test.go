package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
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

func TestNewHandler(t *testing.T) {
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(newStep, simpleMessageHandler())
	require.NotNil(t, h)
	assert.NotNil(t, h.newStep)
	assert.NotNil(t, h.messageHandler)
	assert.NotNil(t, h.store)
}

func TestHandler_ServeMux_Routing(t *testing.T) {
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(newStep, simpleMessageHandler())
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
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(newStep, simpleMessageHandler())

	req := httptest.NewRequest("POST", "/sessions", nil)
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	require.Equal(t, 201, rr.Code)
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	var resp map[string]string
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	require.NotEmpty(t, resp["id"])
	assert.Equal(t, "/sessions/"+resp["id"]+"/events", resp["events_url"])

	// Verify the session exists in the store.
	_, ok := h.store.Get(resp["id"])
	assert.True(t, ok)
}

func TestHandler_CreateSession_StoresStep(t *testing.T) {
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(newStep, simpleMessageHandler())

	req := httptest.NewRequest("POST", "/sessions", nil)
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	require.Equal(t, 201, rr.Code)

	var resp map[string]string
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))

	session, ok := h.store.Get(resp["id"])
	require.True(t, ok)
	assert.NotNil(t, session.step)
	assert.NotNil(t, session.state)
}

func TestHandler_CreateSession_RandFailure(t *testing.T) {
	old := randRead
	randRead = &failReader{}
	defer func() { randRead = old }()

	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(newStep, simpleMessageHandler())

	req := httptest.NewRequest("POST", "/sessions", nil)
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	assert.Equal(t, 500, rr.Code)
}

func TestHandler_DeleteSession(t *testing.T) {
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(newStep, simpleMessageHandler())

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

	// Verify the session no longer exists.
	_, ok := h.store.Get(sessionID)
	assert.False(t, ok)
}

func TestHandler_DeleteSession_NotFound(t *testing.T) {
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(newStep, simpleMessageHandler())

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
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(newStep, turnMessageHandler(prov))

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
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(newStep, simpleMessageHandler())

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
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(newStep, turnMessageHandler(prov))

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
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(newStep, simpleMessageHandler())

	req := httptest.NewRequest("GET", "/sessions/nonexistent/events", nil)
	rr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(rr, req)

	assert.Equal(t, 404, rr.Code)
}

func TestHandler_SessionEvents_ContextCancel(t *testing.T) {
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(newStep, simpleMessageHandler())

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
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(newStep, turnMessageHandler(prov))

	// Create a session.
	createReq := httptest.NewRequest("POST", "/sessions", nil)
	createRr := httptest.NewRecorder()
	h.ServeMux().ServeHTTP(createRr, createReq)
	require.Equal(t, 201, createRr.Code)

	var createResp map[string]string
	require.NoError(t, json.Unmarshal(createRr.Body.Bytes(), &createResp))
	sessionID := createResp["id"]

	// Lock the session manually to simulate an in-progress turn.
	session, _ := h.store.Get(sessionID)
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
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(newStep, simpleMessageHandler())

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
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(newStep, turnMessageHandler(prov))

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
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(newStep, simpleMessageHandler())

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
