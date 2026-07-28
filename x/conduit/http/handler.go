package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	stdhttp "net/http"
	"strings"

	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/x/conduit"

	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// Option configures a Handler via functional options.
type Option func(*Handler)

// WithAddr sets the TCP address for the HTTP server (e.g., ":7654").
// If not specified, the server defaults to ":7654".
func WithAddr(addr string) Option {
	return func(h *Handler) {
		h.addr = addr
	}
}

// WithName sets the application name displayed in the web chat UI
// title and header.
func WithName(name string) Option {
	return func(h *Handler) {
		h.name = name
	}
}

// WithTracer configures an OpenTelemetry tracer for the HTTP handler.
// When configured, incoming requests extract traceparent from headers
// and start a server span, and outgoing SSE responses carry the
// span context.
func WithTracer(tracer trace.Tracer) Option {
	return func(h *Handler) {
		h.tracer = tracer
		h.propagator = propagation.TraceContext{}
	}
}

// WithoutUI disables the embedded web chat UI. Use this when embedding
// the handler in an existing server where the UI routes are not
// desired.
func WithoutUI() Option {
	return func(h *Handler) {
		h.withUI = false
	}
}

// WithUI is retained for legacy callers; the UI is enabled by default
// in New. Calling WithUI explicitly is a no-op.
func WithUI() Option {
	return func(h *Handler) {
		h.withUI = true
	}
}

// Descriptor enumerates the capabilities of the HTTP conduit.
var Descriptor = conduit.Descriptor{
	Name:        "HTTP",
	Description: "HTTP conduit with embedded web chat UI",
	Capabilities: []conduit.Capability{
		conduit.CapEventSource,
		conduit.CapShowStatus,
		conduit.CapRenderTurn,
		conduit.CapRenderMarkdown,
		conduit.CapAudioNotification,
	},
}

// Handler provides HTTP endpoints for the ore framework's session
// primitives. It is mounted on an http.ServeMux via ServeMux().
type Handler struct {
	backend    Backend
	withUI     bool
	addr       string
	name       string
	tracer     trace.Tracer
	propagator propagation.TextMapPropagator
}

// New creates a new HTTP conduit that implements conduit.Conduit.
// The returned value must be started with Start(ctx) to begin serving.
// For advanced use cases (e.g., embedding in an existing http.Server),
// type-assert the returned conduit.Conduit to *Handler and call
// ServeMux().
func New(backend Backend, opts ...Option) (conduit.Conduit, error) {
	if backend == nil {
		return nil, fmt.Errorf("backend is required")
	}
	h := &Handler{backend: backend, withUI: true, name: "ore chat"}
	for _, opt := range opts {
		opt(h)
	}
	if h.addr == "" {
		h.addr = ":7654"
	}
	return h, nil
}

// ServeMux returns an http.ServeMux with all HTTP conduit routes
// registered. This method is exported primarily for table-driven
// unit tests; most callers should use Start(ctx), which creates and
// runs the server internally.
func (h *Handler) ServeMux() *stdhttp.ServeMux {
	mux := stdhttp.NewServeMux()
	mux.HandleFunc("POST /sessions", h.createSession)
	mux.HandleFunc("GET /sessions/{id}", h.getSession)
	mux.HandleFunc("DELETE /sessions/{id}", h.deleteSession)
	mux.HandleFunc("GET /threads", h.listThreads)
	mux.HandleFunc("POST /sessions/{id}/events", h.submitEvent)
	mux.HandleFunc("GET /sessions/{id}/events", h.sessionEvents)
	if h.withUI {
		mux.HandleFunc("GET /", h.serveLanding)
		mux.HandleFunc("GET /chat", h.serveUI)
		mux.HandleFunc("GET /chat.js", h.serveUI)
	}
	return mux
}

// Start creates an http.Server from the Handler's ServeMux and begins
// listening on the configured address. It blocks until ctx is
// cancelled or the server encounters a fatal error. On context
// cancellation the server is shut down gracefully.
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
		<-errCh
		return nil
	case err := <-errCh:
		if err == stdhttp.ErrServerClosed {
			return nil
		}
		return err
	}
}

// createSession handles POST /sessions.
//
// Request body (all fields optional):
//
//	{
//	  "thread_id": "<existing-thread-id>"   // omit for a fresh session
//	}
//
// An empty or absent body is treated as a request to create a
// fresh session (no thread attach).
//
// Response: 201 Created with
//
//	{"id": "<session-id>", "events_url": "/sessions/<id>/events"}
func (h *Handler) createSession(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	var req struct {
		ThreadID string `json:"thread_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// An empty body is valid (no thread_id). Any other decode
		// error is malformed JSON.
		if !errors.Is(err, io.EOF) {
			writeJSONError(w, stdhttp.StatusBadRequest, "invalid request body")
			return
		}
	}

	sess, err := h.backend.CreateSession(r.Context(), req.ThreadID)
	if err != nil {
		if errors.Is(err, session.ErrSessionNotFound) {
			writeJSONError(w, stdhttp.StatusNotFound, "thread not found")
			return
		}
		writeJSONError(w, stdhttp.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, stdhttp.StatusCreated, map[string]string{
		"id":         sess.ID(),
		"events_url": "/sessions/" + sess.ID() + "/events",
	})
}

// getSession handles GET /sessions/{id}.
//
// Response: 200 OK with session metadata (id + thread id when
// applicable).
func (h *Handler) getSession(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	id := r.PathValue("id")

	sess, err := h.backend.GetSession(r.Context(), id)
	if err != nil {
		if errors.Is(err, session.ErrSessionNotFound) {
			writeJSONError(w, stdhttp.StatusNotFound, "session not found")
			return
		}
		writeJSONError(w, stdhttp.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, stdhttp.StatusOK, map[string]string{
		"id": sess.ID(),
	})
}

// deleteSession handles DELETE /sessions/{id}. The thread itself is
// preserved; the session is removed from the active registry.
func (h *Handler) deleteSession(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	id := r.PathValue("id")

	if err := h.backend.DeleteSession(r.Context(), id); err != nil {
		if errors.Is(err, session.ErrSessionNotFound) {
			writeJSONError(w, stdhttp.StatusNotFound, "session not found")
			return
		}
		writeJSONError(w, stdhttp.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(stdhttp.StatusNoContent)
}

// submitEvent handles POST /sessions/{id}/events.
//
// Request body:
//
//	{
//	  "kind":    "user_message" | "interrupt",
//	  "content": "..."                       // only for user_message
//	}
//
// The session is resolved first; a missing session returns 404
// before the body is parsed. This mirrors the routing-test
// expectation that an unknown session ID is the dominant error.
//
// Response: 202 Accepted on admission. The handler does not block on
// inference; the application's admission policy (e.g., the engine's
// bounded queue) governs the outcome. Errors that surface here are
// the application's: queue full, session not registered, etc.
func (h *Handler) submitEvent(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	id := r.PathValue("id")

	ctx := r.Context()
	if h.propagator != nil {
		ctx = h.propagator.Extract(ctx, propagation.HeaderCarrier(r.Header))
	}

	if _, err := h.backend.GetSession(ctx, id); err != nil {
		if errors.Is(err, session.ErrSessionNotFound) {
			writeJSONError(w, stdhttp.StatusNotFound, "session not found")
			return
		}
		writeJSONError(w, stdhttp.StatusInternalServerError, err.Error())
		return
	}

	var req struct {
		Kind    string `json:"kind"`
		Content string `json:"content,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if !errors.Is(err, io.EOF) {
			writeJSONError(w, stdhttp.StatusBadRequest, "invalid request body")
			return
		}
	}

	var event session.Event
	switch req.Kind {
	case "user_message":
		event = session.UserMessageEvent{
			Content: req.Content,
			Ctx:     loop.WithProvenance(ctx, "http"),
		}
	case "interrupt":
		event = session.InterruptEvent{
			Ctx: loop.WithProvenance(ctx, "http"),
		}
	case "":
		// Empty body: treat as interrupt, the minimal "cancel anything
		// in-flight" signal.
		event = session.InterruptEvent{
			Ctx: loop.WithProvenance(ctx, "http"),
		}
	default:
		writeJSONError(w, stdhttp.StatusBadRequest, fmt.Sprintf("unknown event kind: %q", req.Kind))
		return
	}

	if err := h.backend.Submit(ctx, id, event); err != nil {
		if errors.Is(err, session.ErrSessionNotFound) {
			writeJSONError(w, stdhttp.StatusNotFound, "session not found")
			return
		}
		writeJSONError(w, stdhttp.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(stdhttp.StatusAccepted)
}

// sessionEvents handles GET /sessions/{id}/events. It opens an SSE
// stream from the session's authoritative output channel. The query
// parameter ?kinds=... (comma-separated) filters by event kind; the
// default is all kinds. The stream closes when the client
// disconnects, the session is closed, or the request context is
// cancelled.
func (h *Handler) sessionEvents(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	id := r.PathValue("id")

	kinds := defaultEventKinds
	if k := r.URL.Query().Get("kinds"); k != "" {
		kinds = strings.Split(k, ",")
	}

	sess, err := h.backend.GetSession(r.Context(), id)
	if err != nil {
		if errors.Is(err, session.ErrSessionNotFound) {
			writeJSONError(w, stdhttp.StatusNotFound, "session not found")
			return
		}
		writeJSONError(w, stdhttp.StatusInternalServerError, err.Error())
		return
	}

	sub := sess.Subscribe(kinds...)

	sw, err := newSSEWriter(w)
	if err != nil {
		writeJSONError(w, stdhttp.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Flush response headers before any events arrive so the
	// client's Do() returns promptly rather than blocking on the
	// first byte.
	_ = sw.WriteComment("connected")

	for {
		select {
		case event, ok := <-sub:
			if !ok {
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

// defaultEventKinds is the default set of kinds streamed by
// /sessions/{id}/events when ?kinds= is not specified.
var defaultEventKinds = []string{
	"text_delta", "reasoning_delta", "tool_call", "tool_result",
	"turn_complete", "error", "properties", "lifecycle", "notice", "activity",
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w stdhttp.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeJSONError writes a JSON error response with the given status
// code and message.
func writeJSONError(w stdhttp.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
