package http

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockProvider is a minimal provider.Provider implementation for testing.
type mockProvider struct{}

func (m *mockProvider) Invoke(ctx context.Context, s state.State, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
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
		{"send message", "POST", "/sessions/abc-123/messages", 501},
		{"session events", "GET", "/sessions/abc-123/events", 501},
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
