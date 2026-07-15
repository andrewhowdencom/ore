package anthropic

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sync/atomic"

	"github.com/andrewhowdencom/ore/x/provider/mock"
)

// messagesPath is the wire path the Anthropic SDK targets. The SDK
// always uses "v1/messages" relative to the configured base URL; the
// mock registers the absolute path so callers don't need to reason
// about base-URL suffixes.
const messagesPath = "/v1/messages"

// defaultModel is the model name echoed in the response when the
// request body omits or fails to parse "model".
const defaultModel = "claude-3-7-sonnet-latest"

// defaultStopReason matches the canonical Anthropic stop_reason value
// emitted when the model completes naturally.
const defaultStopReason = "end_turn"

// ---------------------------------------------------------------------------
// Configuration options.
// ---------------------------------------------------------------------------

// Option configures a Server via the functional-options pattern.
type Option func(*config)

type config struct {
	queue *mock.Queue
}

// WithResponses sets the canned response queue. Successive HTTP
// requests rotate through the supplied [mock.Response] values.
func WithResponses(rs ...mock.Response) Option {
	return func(c *config) {
		c.queue = mock.NewQueue(rs...)
	}
}

// ---------------------------------------------------------------------------
// Server.
// ---------------------------------------------------------------------------

// Server is a wire-compatible Anthropic Messages streaming mock.
type Server struct {
	q       *mock.Queue
	idCount atomic.Uint64
	handler http.Handler
}

// New constructs an Anthropic mock server.
func New(opts ...Option) (*Server, error) {
	cfg := &config{}
	for _, opt := range opts {
		opt(cfg)
	}
	if cfg.queue == nil {
		cfg.queue = mock.NewQueue()
	}

	s := &Server{q: cfg.queue}
	mux := http.NewServeMux()
	mux.HandleFunc(messagesPath, s.handleMessages)
	s.handler = mux
	return s, nil
}

// Handler returns the HTTP handler suitable for `httptest.NewServer`.
func (s *Server) Handler() http.Handler {
	return s.handler
}

// Start binds a listener on addr and serves until the listener closes.
// The bound address is printed to stderr in the format
// "mock-server(anthropic): listening on http://<host>:<port>".
func (s *Server) Start(addr string) error {
	srv := &http.Server{Handler: s.handler}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("anthropic mock: listen %q: %w", addr, err)
	}
	fmt.Fprintf(os.Stderr, "mock-server(anthropic): listening on http://%s\n", ln.Addr().String())
	return srv.Serve(ln)
}

// ---------------------------------------------------------------------------
// HTTP handler.
// ---------------------------------------------------------------------------

// handleMessages is the HTTP entry point. It reads the model name
// from the request body and writes the SSE stream for the next
// canned response.
func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	model := s.readModel(r)
	resp := s.q.Next()

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("anthropic-version", "2023-06-01")
	w.WriteHeader(http.StatusOK)

	id := s.nextID()

	// 1. message_start — envelope with the initial usage block.
	s.writeEvent(w, "message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":            id,
			"type":          "message",
			"role":          "assistant",
			"content":       []any{},
			"model":         model,
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage":         s.usageFromResponse(resp),
		},
	})

	// 2. content_block_* for thinking, text, and tool_use blocks.
	// Index counter increments per block so the SDK can reassemble
	// them in arrival order.
	idx := 0

	if resp.Reasoning != "" {
		idx = s.emitThinking(w, idx, resp.Reasoning, resp.Signature)
	}

	if resp.Text != "" {
		s.emitText(w, idx, resp.Text)
		idx++
	}

	for _, tc := range resp.ToolCalls {
		s.emitToolUse(w, idx, tc)
		idx++
	}

	// 3. message_delta — carries stop_reason and the final usage.
	stopReason := resp.StopReason
	if stopReason == "" {
		stopReason = defaultStopReason
	}

	finalUsage := map[string]any{}
	if resp.Usage != nil {
		finalUsage = map[string]any{
			"input_tokens":                resp.Usage.PromptTokens,
			"output_tokens":               resp.Usage.CompletionTokens,
			"cache_creation_input_tokens": 0,
			"cache_read_input_tokens":     0,
		}
	}

	s.writeEvent(w, "message_delta", map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   stopReason,
			"stop_sequence": nil,
		},
		"usage": finalUsage,
	})

	// 4. message_stop — terminator. The SDK breaks out of its loop
	// on this event and emits any buffered artifacts.
	s.writeEvent(w, "message_stop", map[string]any{"type": "message_stop"})

	flusher.Flush()
}

// emitThinking writes a thinking block: content_block_start +
// thinking_delta(s) + (optional signature_delta) + content_block_stop.
// Returns the next free block index.
func (s *Server) emitThinking(w http.ResponseWriter, idx int, reasoning, signature string) int {
	s.writeEvent(w, "content_block_start", map[string]any{
		"type":          "content_block_start",
		"index":         idx,
		"content_block": map[string]any{"type": "thinking", "thinking": ""},
	})

	s.writeEvent(w, "content_block_delta", map[string]any{
		"type":  "content_block_delta",
		"index": idx,
		"delta": map[string]any{
			"type":     "thinking_delta",
			"thinking": reasoning,
		},
	})

	if signature != "" {
		s.writeEvent(w, "content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": idx,
			"delta": map[string]any{
				"type":      "signature_delta",
				"signature": signature,
			},
		})
	}

	s.writeEvent(w, "content_block_stop", map[string]any{
		"type":  "content_block_stop",
		"index": idx,
	})

	return idx + 1
}

// emitText writes a text block. Returns the next free block index.
func (s *Server) emitText(w http.ResponseWriter, idx int, text string) int {
	s.writeEvent(w, "content_block_start", map[string]any{
		"type":          "content_block_start",
		"index":         idx,
		"content_block": map[string]any{"type": "text", "text": ""},
	})

	s.writeEvent(w, "content_block_delta", map[string]any{
		"type":  "content_block_delta",
		"index": idx,
		"delta": map[string]any{
			"type": "text_delta",
			"text": text,
		},
	})

	s.writeEvent(w, "content_block_stop", map[string]any{
		"type":  "content_block_stop",
		"index": idx,
	})

	return idx
}

// emitToolUse writes a tool_use block: content_block_start with id/name
// + input_json_delta carrying the partial arguments + content_block_stop.
func (s *Server) emitToolUse(w http.ResponseWriter, idx int, tc mock.ToolCall) {
	s.writeEvent(w, "content_block_start", map[string]any{
		"type":          "content_block_start",
		"index":         idx,
		"content_block": map[string]any{"type": "tool_use", "id": tc.ID, "name": tc.Name, "input": map[string]any{}},
	})

	s.writeEvent(w, "content_block_delta", map[string]any{
		"type":  "content_block_delta",
		"index": idx,
		"delta": map[string]any{
			"type":        "input_json_delta",
			"partial_json": tc.Arguments,
		},
	})

	s.writeEvent(w, "content_block_stop", map[string]any{
		"type":  "content_block_stop",
		"index": idx,
	})
}

// usageFromResponse produces the initial-usage map for message_start.
// When the canned response has no usage, returns zeros so the SDK's
// read-side doesn't choke on missing fields.
func (s *Server) usageFromResponse(resp mock.Response) map[string]any {
	if resp.Usage != nil {
		return map[string]any{
			"input_tokens":                resp.Usage.PromptTokens,
			"output_tokens":               resp.Usage.CompletionTokens,
			"cache_creation_input_tokens": 0,
			"cache_read_input_tokens":     0,
		}
	}
	return map[string]any{
		"input_tokens":                0,
		"output_tokens":               0,
		"cache_creation_input_tokens": 0,
		"cache_read_input_tokens":     0,
	}
}

// readModel extracts the model name from the request body, falling
// back to [defaultModel] on parse error or absence.
func (s *Server) readModel(r *http.Request) string {
	if r.Body == nil {
		return defaultModel
	}
	defer func() { _, _ = io.Copy(io.Discard, r.Body) }()
	var req struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err == nil && req.Model != "" {
		return req.Model
	}
	return defaultModel
}

// writeEvent serializes the payload as JSON and writes an SSE frame
// (event + data, terminated by a blank line).
func (s *Server) writeEvent(w http.ResponseWriter, event string, payload any) {
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
}

// nextID generates a deterministic-ish message id. Counter ensures
// uniqueness across concurrent requests; random suffix matches the
// visual shape of real Anthropic responses.
func (s *Server) nextID() string {
	var buf [8]byte
	_, _ = rand.Read(buf[:])
	n := s.idCount.Add(1)
	return fmt.Sprintf("msg_mock_%d_%s", n, hex.EncodeToString(buf[:]))
}