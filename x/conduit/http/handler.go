package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	stdhttp "net/http"
	"strings"
	"time"

	"github.com/andrewhowdencom/ore/x/conduit"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/state"
	"github.com/andrewhowdencom/ore/thread"
)

// Option configures a Handler via functional options.
type Option func(*Handler)

// WithUI enables serving of an embedded HTML/JS chat client at GET / and
// GET /chat.js. When enabled, the handler registers these routes in ServeMux.
func WithUI() Option {
	return func(h *Handler) {
		h.withUI = true
	}
}

// WithAddr sets the TCP address for the HTTP server (e.g., ":8080").
// If not specified, the server defaults to ":8080".
func WithAddr(addr string) Option {
	return func(h *Handler) {
		h.addr = addr
	}
}

// Handler provides HTTP endpoints for the ore framework's thread
// primitives. It is mounted on an http.ServeMux via ServeMux().
type Handler struct {
	mgr    *session.Manager
	withUI bool
	addr   string
}

// New creates a new HTTP conduit that implements conduit.Conduit.
// The returned value must be started with Start(ctx) to begin serving.
// For advanced use cases (e.g., embedding in an existing http.Server),
// type-assert the returned conduit.Conduit to *Handler and call ServeMux().
func New(mgr *session.Manager, opts ...Option) (conduit.Conduit, error) {
	if mgr == nil {
		return nil, fmt.Errorf("session manager is required")
	}
	h := &Handler{mgr: mgr}
	for _, opt := range opts {
		opt(h)
	}
	if h.addr == "" {
		h.addr = ":8080"
	}
	return h, nil
}

// ServeMux returns an http.ServeMux with all HTTP conduit routes registered.
// Routes include POST /sessions, DELETE /sessions/{id}, POST /messages,
// GET /events, and GET /threads. When WithUI() is enabled, GET / and
// GET /chat.js are also registered for the embedded web client.
// This method is exported primarily for table-driven unit tests; most
// callers should use Start(ctx) which creates and runs the server internally.
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

// Start creates an http.Server from the Handler's ServeMux and begins
// listening on the configured address. It blocks until ctx is cancelled
// or the server encounters a fatal error. On context cancellation the
// server is shut down gracefully.
func (h *Handler) Start(ctx context.Context) error {
	server := &stdhttp.Server{
		Addr:    h.addr,
		Handler: h.ServeMux(),
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		_ = server.Shutdown(context.Background())
		<-errCh // wait for ListenAndServe to return
		return nil
	case err := <-errCh:
		if err == stdhttp.ErrServerClosed {
			return nil
		}
		return err
	}
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

	var stream *session.Stream
	var err error

	if req.ThreadID != "" {
		stream, err = h.mgr.Attach(req.ThreadID)
		if err != nil {
			w.WriteHeader(stdhttp.StatusNotFound)
			return
		}
	} else {
		stream, err = h.mgr.Create()
		if err != nil {
			w.WriteHeader(stdhttp.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(stdhttp.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"id":         stream.ID(),
		"events_url": "/sessions/" + stream.ID() + "/events",
	})
}

// deleteSession handles DELETE /sessions/{id} by removing the session and
// closing its Step. The thread is NOT deleted from the store.
func (h *Handler) deleteSession(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	id := r.PathValue("id")

	if err := h.mgr.Close(id); err != nil {
		w.WriteHeader(stdhttp.StatusNotFound)
		return
	}

	w.WriteHeader(stdhttp.StatusNoContent)
}

// sendMessage handles POST /sessions/{id}/messages by running the inference
// pipeline through the session manager and streaming events as NDJSON.
func (h *Handler) sendMessage(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	id := r.PathValue("id")

	// Verify the session exists and is not busy before starting the response.
	if err := h.mgr.Check(id); err != nil {
		if errors.Is(err, session.ErrSessionBusy) {
			w.WriteHeader(stdhttp.StatusConflict)
		} else {
			w.WriteHeader(stdhttp.StatusNotFound)
		}
		return
	}

	stream, err := h.mgr.Get(id)
	if err != nil {
		w.WriteHeader(stdhttp.StatusInternalServerError)
		return
	}

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
		req.Kinds = []string{"text", "reasoning", "tool_call", "tool_result", "turn_complete", "error"}
	}

	// Subscribe to the session's FanOut before the goroutine starts.
	subCh, err := stream.Subscribe(req.Kinds...)
	if err != nil {
		w.WriteHeader(stdhttp.StatusInternalServerError)
		return
	}

	// Run the inference pipeline in a goroutine.
	done := make(chan error)
	go func() {
		err := stream.Process(r.Context(), session.UserMessageEvent{Content: req.Content})
		select {
		case done <- err:
		case <-r.Context().Done():
		}
	}()

	// Setup NDJSON writer.
	nw, err := newNDJSONWriter(w)
	if err != nil {
		w.WriteHeader(stdhttp.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")

	// Stream events from the subscription until the pipeline completes.
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
			// turn_complete (assistant) is written but we don't return here;
			// we wait for done to fire so that any post-turn error (e.g. Save)
			// is not lost. With unbuffered s.events, done cannot fire until
			// after the FanOut has delivered all events to subCh.
		case err := <-done:
			// Drain any remaining events that have already been delivered
			// to the subscription buffer before returning.
			for {
				select {
				case event := <-subCh:
					data, _ := MarshalOutputEvent(event)
					if data != nil {
						_ = nw.WriteEvent(data)
					}
					if tc, ok := event.(loop.TurnCompleteEvent); ok && tc.Turn.Role == state.RoleAssistant {
						if err != nil {
							data, _ := MarshalOutputEvent(loop.ErrorEvent{Err: err})
							_ = nw.WriteEvent(data)
						}
						return
					}
				default:
					if err != nil {
						data, _ := MarshalOutputEvent(loop.ErrorEvent{Err: err})
						_ = nw.WriteEvent(data)
					}
					return
				}
			}
		case <-r.Context().Done():
			// Client disconnected; stop streaming.
			return
		}
	}
}

// sessionEvents handles GET /sessions/{id}/events by establishing a persistent
// SSE connection that streams events from the session's FanOut.
func (h *Handler) sessionEvents(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	id := r.PathValue("id")

	// Parse kinds from query parameter.
	var kinds []string
	if k := r.URL.Query().Get("kinds"); k != "" {
		kinds = strings.Split(k, ",")
	}
	// Default event kinds when none specified.
	if len(kinds) == 0 {
		kinds = []string{"text", "reasoning", "tool_call", "tool_result", "turn_complete", "error"}
	}

	// Subscribe to the session's FanOut.
	stream, err := h.mgr.Get(id)
	if err != nil {
		w.WriteHeader(stdhttp.StatusNotFound)
		return
	}
	subCh, err := stream.Subscribe(kinds...)
	if err != nil {
		w.WriteHeader(stdhttp.StatusInternalServerError)
		return
	}

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
	var threads []*thread.Thread
	var err error
	threads, err = h.mgr.Store().List()
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
	for i, thr := range threads {
		result[i] = summary{
			ID:        thr.ID,
			CreatedAt: thr.CreatedAt,
			UpdatedAt: thr.UpdatedAt,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}
