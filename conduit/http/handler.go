package http

import (
	"context"
	_ "embed"
	"encoding/json"
	stdhttp "net/http"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/andrewhowdencom/ore/thread"
	"github.com/andrewhowdencom/ore/loop"
)

// MessageHandler processes a user message within a locked session.
// It receives the session (with its Step and State), the parsed message
// content, and the request context. The handler may call session.Step().Submit(),
// session.Step().Turn(), or run any cognitive pattern. Events emitted by the
// Step's FanOut are streamed to the client automatically by the Handler.
// The handler runs in a goroutine; it should return when processing is complete.
type MessageHandler func(ctx context.Context, session *Session, content string) error

// Option configures a Handler via functional options.
type Option func(*Handler)

// WithUI enables serving of an embedded HTML/JS chat client at GET / and
// GET /chat.js. When enabled, the handler registers these routes in ServeMux.
func WithUI() Option {
	return func(h *Handler) {
		h.withUI = true
	}
}

// Handler provides HTTP endpoints for the ore framework's thread
// primitives. It is mounted on an http.ServeMux via ServeMux().
type Handler struct {
	// threadStore is the shared thread persistence layer.
	threadStore thread.Store
	// newStep is called once per session to create an isolated Step.
	newStep func() *loop.Step
	// messageHandler processes each incoming user message within a locked session.
	messageHandler MessageHandler
	// sessions tracks active HTTP sessions keyed by thread ID.
	sessions map[string]*Session
	// mu protects the sessions map.
	mu sync.RWMutex
	withUI         bool
}

// NewHandler creates a new Handler with the given thread store,
// Step factory, and message handler.
func NewHandler(store thread.Store, newStep func() *loop.Step, messageHandler MessageHandler, opts ...Option) *Handler {
	h := &Handler{
		threadStore:      store,
		newStep:        newStep,
		messageHandler: messageHandler,
		sessions:       make(map[string]*Session),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// ServeMux returns an http.ServeMux with all HTTP conduit routes registered.
func (h *Handler) ServeMux() *stdhttp.ServeMux {
	mux := stdhttp.NewServeMux()
	mux.HandleFunc("POST /sessions", h.createSession)
	mux.HandleFunc("DELETE /sessions/{id}", h.deleteSession)
	mux.HandleFunc("POST /sessions/{id}/messages", h.sendMessage)
	mux.HandleFunc("GET /sessions/{id}/events", h.sessionEvents)
	mux.HandleFunc("GET /threads", h.listThreads)
	if h.withUI {
		mux.HandleFunc("GET /", h.serveUI)
		mux.HandleFunc("GET /chat.js", h.serveUI)
	}
	return mux
}

// createSession handles POST /sessions by creating a new ephemeral session.
// If a "thread_id" is provided in the JSON body, the session attaches
// to an existing thread. On success it responds with 201 Created and a
// JSON body:
//
//	{"id": "<session-id>", "events_url": "/sessions/<session-id>/events"}
func (h *Handler) createSession(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var req struct {
		ThreadID string `json:"thread_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// An empty body is valid (creates a new session).
		// Any non-EOF error means malformed JSON.
		if err != io.EOF {
			w.WriteHeader(stdhttp.StatusBadRequest)
			return
		}
	}

	var thread *thread.Thread
	var err error

	if req.ThreadID != "" {
		var ok bool
		thread, ok = h.threadStore.Get(req.ThreadID)
		if !ok {
			w.WriteHeader(stdhttp.StatusNotFound)
			return
		}
	} else {
		thread, err = h.threadStore.Create()
		if err != nil {
			w.WriteHeader(stdhttp.StatusInternalServerError)
			return
		}
	}

	step := h.newStep()
	session := &Session{
		id:     thread.ID,
		thread: thread,
		step:   step,
	}

	h.mu.Lock()
	h.sessions[thread.ID] = session
	h.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(stdhttp.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"id":         session.id,
		"events_url": "/sessions/" + session.id + "/events",
	})
}

// deleteSession handles DELETE /sessions/{id} by removing the session and
// closing its Step. The thread is NOT deleted from the store.
func (h *Handler) deleteSession(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	id := r.PathValue("id")

	h.mu.Lock()
	session, ok := h.sessions[id]
	delete(h.sessions, id)
	h.mu.Unlock()

	if !ok {
		w.WriteHeader(stdhttp.StatusNotFound)
		return
	}

	if session.step != nil {
		_ = session.step.Close()
	}

	w.WriteHeader(stdhttp.StatusNoContent)
}

// sendMessage handles POST /sessions/{id}/messages by invoking the
// configured messageHandler and streaming events as NDJSON.
func (h *Handler) sendMessage(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	id := r.PathValue("id")

	h.mu.RLock()
	session, ok := h.sessions[id]
	h.mu.RUnlock()

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

	// Capture turn count before the messageHandler runs so we can return all new turns.
	beforeCount := len(session.State().Turns())

	// Subscribe to the session's FanOut before the goroutine starts.
	subCh := session.step.Subscribe(req.Kinds...)

	// Run the messageHandler in a goroutine so the main goroutine can stream
	// events from the subscription channel.
	done := make(chan error, 1)
	go func() {
		done <- h.messageHandler(r.Context(), session, req.Content)
	}()

	// Setup NDJSON writer.
	nw, err := newNDJSONWriter(w)
	if err != nil {
		w.WriteHeader(stdhttp.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")

	// Stream events from the subscription until the messageHandler completes.
	for {
		select {
		case event := <-subCh:
			data, err := MarshalOutputEvent(event)
			if err != nil {
				// Skip events that can't be marshaled.
				continue
			}
			if data == nil {
				// Skip unknown artifact kinds (e.g., custom extensions).
				continue
			}
			if err := nw.WriteEvent(data); err != nil {
				// Client likely disconnected.
				return
			}
		case err := <-done:
			// Drain any remaining events from the subscription buffer.
			drainSubscription(subCh, nw)
			// Stream a final error event if the handler failed.
			if err != nil {
				data, _ := MarshalOutputEvent(loop.ErrorEvent{Err: err})
				_ = nw.WriteEvent(data)
			}
			// Save thread state; stream an error if persistence fails.
			if saveErr := h.threadStore.Save(session.thread); saveErr != nil {
				data, _ := MarshalOutputEvent(loop.ErrorEvent{Err: saveErr})
				_ = nw.WriteEvent(data)
			}
			// Stream the complete event with all new turns.
			newTurns := session.State().Turns()[beforeCount:]
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
			if data == nil {
				// Skip unknown artifact kinds (e.g., custom extensions).
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

	h.mu.RLock()
	session, ok := h.sessions[id]
	h.mu.RUnlock()

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

// serveUI serves the embedded static files (index.html and chat.js) for the
// web chat client. It reads the requested file from staticFS and returns 404
// for unknown paths. It is registered at GET / and GET /chat.js when WithUI()
// is enabled.
func (h *Handler) serveUI(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	switch r.URL.Path {
	case "/":
		data, err := staticFS.ReadFile("static/index.html")
		if err != nil {
			w.WriteHeader(stdhttp.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
	case "/chat.js":
		data, err := staticFS.ReadFile("static/chat.js")
		if err != nil {
			w.WriteHeader(stdhttp.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		_, _ = w.Write(data)
	default:
		w.WriteHeader(stdhttp.StatusNotFound)
	}
}

// listThreads handles GET /threads by returning all threads
// in the store as a JSON array.
func (h *Handler) listThreads(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	threads, err := h.threadStore.List()
	if err != nil {
		w.WriteHeader(stdhttp.StatusInternalServerError)
		return
	}

	type summary struct {
		ID        string    `json:"id"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
	}

	result := make([]summary, len(threads))
	for i, thread := range threads {
		result[i] = summary{
			ID:        thread.ID,
			CreatedAt: thread.CreatedAt,
			UpdatedAt: thread.UpdatedAt,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}
