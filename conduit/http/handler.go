package http

import (
	"encoding/json"
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

// createSession handles POST /sessions by creating a new ephemeral session.
func (h *Handler) createSession(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	step := h.newStep()
	session, err := h.store.Create(step)
	if err != nil {
		w.WriteHeader(stdhttp.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(stdhttp.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"id":         session.id,
		"events_url": "/sessions/" + session.id + "/events",
	})
}

// deleteSession handles DELETE /sessions/{id} by removing the session and
// closing its Step.
func (h *Handler) deleteSession(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	id := r.PathValue("id")
	if ok := h.store.Delete(id); !ok {
		w.WriteHeader(stdhttp.StatusNotFound)
		return
	}
	w.WriteHeader(stdhttp.StatusNoContent)
}

// sendMessage handles POST /sessions/{id}/messages. Stub: returns 501 Not Implemented.
func (h *Handler) sendMessage(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	w.WriteHeader(stdhttp.StatusNotImplemented)
}

// sessionEvents handles GET /sessions/{id}/events. Stub: returns 501 Not Implemented.
func (h *Handler) sessionEvents(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	w.WriteHeader(stdhttp.StatusNotImplemented)
}
