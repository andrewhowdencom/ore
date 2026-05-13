package http

import (
	"encoding/json"
	stdhttp "net/http"
	"strings"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/cognitive"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/state"
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

// sendMessage handles POST /sessions/{id}/messages by submitting the user message,
// running the full server-side ReAct loop, and streaming events as NDJSON.
func (h *Handler) sendMessage(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	id := r.PathValue("id")
	session, ok := h.store.Get(id)
	if !ok {
		w.WriteHeader(stdhttp.StatusNotFound)
		return
	}

	if !session.Lock() {
		w.WriteHeader(stdhttp.StatusConflict)
		return
	}
	defer session.Unlock()

	// Parse request body.
	var req struct {
		Content string   `json:"content"`
		Kinds   []string `json:"kinds,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(stdhttp.StatusBadRequest)
		return
	}

	// Default event kinds when none specified.
	if len(req.Kinds) == 0 {
		req.Kinds = []string{"text_delta", "reasoning_delta", "tool_call_delta", "turn_complete", "error"}
	}

	// Capture turn count before submitting so we can return all new turns.
	beforeCount := len(session.state.Turns())

	// Subscribe to the session's FanOut before submitting, so we capture
	// all events from Submit and the subsequent ReAct loop.
	subCh := session.step.Subscribe(req.Kinds...)

	// Submit the user message as a non-inference turn.
	if _, err := session.step.Submit(r.Context(), session.state, state.RoleUser, artifact.Text{Content: req.Content}); err != nil {
		w.WriteHeader(stdhttp.StatusInternalServerError)
		return
	}

	// Setup NDJSON writer.
	nw, err := newNDJSONWriter(w)
	if err != nil {
		w.WriteHeader(stdhttp.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")

	// Run the ReAct loop in a goroutine so the main goroutine can stream
	// events from the subscription channel.
	done := make(chan error, 1)
	go func() {
		react := &cognitive.ReAct{
			Step:     session.step,
			Provider: h.provider,
		}
		_, err := react.Run(r.Context(), session.state)
		done <- err
	}()

	// Stream events from the subscription until the ReAct loop completes.
	for {
		select {
		case event := <-subCh:
			data, err := MarshalOutputEvent(event)
			if err != nil {
				// Skip events that can't be marshaled.
				continue
			}
			if err := nw.WriteEvent(data); err != nil {
				// Client likely disconnected.
				return
			}
		case err := <-done:
			// Drain any remaining events from the subscription buffer.
			drainSubscription(subCh, nw)
			// Stream a final error event if the ReAct loop failed.
			if err != nil {
				data, _ := MarshalOutputEvent(loop.ErrorEvent{Err: err})
				_ = nw.WriteEvent(data)
			}
			// Stream the complete event with all new turns.
			newTurns := session.state.Turns()[beforeCount:]
			data, _ := MarshalCompleteEvent(newTurns)
			_ = nw.WriteEvent(data)
			return
		case <-r.Context().Done():
			// Client disconnected; stop streaming.
			return
		}
	}
}

// drainSubscription reads all currently buffered events from the subscription
// channel and writes them to the NDJSON writer. It is non-blocking and returns
// as soon as the buffer is empty.
func drainSubscription(subCh <-chan loop.OutputEvent, nw *ndjsonWriter) {
	for {
		select {
		case event := <-subCh:
			data, err := MarshalOutputEvent(event)
			if err != nil {
				continue
			}
			_ = nw.WriteEvent(data)
		default:
			return
		}
	}
}

// sessionEvents handles GET /sessions/{id}/events by establishing a persistent
// SSE connection that streams events from the session's FanOut.
func (h *Handler) sessionEvents(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	id := r.PathValue("id")
	session, ok := h.store.Get(id)
	if !ok {
		w.WriteHeader(stdhttp.StatusNotFound)
		return
	}

	// Parse kinds from query parameter.
	var kinds []string
	if k := r.URL.Query().Get("kinds"); k != "" {
		kinds = strings.Split(k, ",")
	}
	// Default event kinds when none specified.
	if len(kinds) == 0 {
		kinds = []string{"text_delta", "reasoning_delta", "tool_call_delta", "turn_complete", "error"}
	}

	// Subscribe to the session's FanOut.
	subCh := session.step.Subscribe(kinds...)

	// Setup SSE writer.
	sw, err := newSSEWriter(w)
	if err != nil {
		w.WriteHeader(stdhttp.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Read from subscription until client disconnects or session closes.
	for {
		select {
		case event, ok := <-subCh:
			if !ok {
				// Subscription channel closed (session deleted).
				return
			}
			data, err := MarshalOutputEvent(event)
			if err != nil {
				continue
			}
			if err := sw.WriteEvent(event.Kind(), data); err != nil {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}
