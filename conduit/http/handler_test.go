package http

import (
	"context"
	"encoding/json"
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
// configured to emit a sequence of artifacts.
type mockProvider struct {
	artifacts []artifact.Artifact
}

func (m *mockProvider) Invoke(ctx context.Context, s state.State, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
	for _, art := range m.artifacts {
		select {
		case ch <- art:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func TestNewHandler(t *testing.T) {
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(&mockProvider{}, newStep)
	require.NotNil(t, h)
	assert.NotNil(t, h.provider)
	assert.NotNil(t, h.newStep)
	assert.NotNil(t, h.store)
}

func TestHandler_ServeMux_Routing(t *testing.T) {
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(&mockProvider{}, newStep)
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
		{"session events stub", "GET", "/sessions/abc-123/events", 501},
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
	h := NewHandler(&mockProvider{}, newStep)

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
	h := NewHandler(&mockProvider{}, newStep)

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

func TestHandler_DeleteSession(t *testing.T) {
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(&mockProvider{}, newStep)

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
	h := NewHandler(&mockProvider{}, newStep)

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
	h := NewHandler(prov, newStep)

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
	h := NewHandler(&mockProvider{}, newStep)

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
	h := NewHandler(prov, newStep)

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

func TestHandler_ServeMux_UnknownPaths(t *testing.T) {
	newStep := func() *loop.Step { return loop.New() }
	h := NewHandler(&mockProvider{}, newStep)

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
