package http

import (
	"context"
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
		{"create session", "POST", "/sessions", 501},
		{"delete session", "DELETE", "/sessions/abc-123", 501},
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
