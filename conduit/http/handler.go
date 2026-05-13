package http

import (
	stdhttp "net/http"

	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/provider"
)

// Handler provides HTTP endpoints for the ore framework's conversation
// primitives. It is mounted on an http.ServeMux via ServeMux().
type Handler struct {
	provider provider.Provider
	newStep  func() *loop.Step
	store    *SessionStore
}

// NewHandler creates a new Handler with the given provider and Step factory.
// The Step factory is called once per session to create an isolated Step
// with its own FanOut.
func NewHandler(p provider.Provider, newStep func() *loop.Step) *Handler {
	return &Handler{
		provider: p,
		newStep:  newStep,
		store:    NewSessionStore(),
	}
}

// ServeMux returns an http.ServeMux with all HTTP conduit routes registered.
func (h *Handler) ServeMux() *stdhttp.ServeMux {
	mux := stdhttp.NewServeMux()
	mux.HandleFunc("POST /sessions", h.createSession)
	mux.HandleFunc("DELETE /sessions/{id}", h.deleteSession)
	mux.HandleFunc("POST /sessions/{id}/messages", h.sendMessage)
	mux.HandleFunc("GET /sessions/{id}/events", h.sessionEvents)
	return mux
}

// createSession handles POST /sessions. Stub: returns 501 Not Implemented.
func (h *Handler) createSession(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	w.WriteHeader(stdhttp.StatusNotImplemented)
}

// deleteSession handles DELETE /sessions/{id}. Stub: returns 501 Not Implemented.
func (h *Handler) deleteSession(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	w.WriteHeader(stdhttp.StatusNotImplemented)
}

// sendMessage handles POST /sessions/{id}/messages. Stub: returns 501 Not Implemented.
func (h *Handler) sendMessage(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	w.WriteHeader(stdhttp.StatusNotImplemented)
}

// sessionEvents handles GET /sessions/{id}/events. Stub: returns 501 Not Implemented.
func (h *Handler) sessionEvents(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	w.WriteHeader(stdhttp.StatusNotImplemented)
}
