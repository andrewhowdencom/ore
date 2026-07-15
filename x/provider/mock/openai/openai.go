package openai

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
	"time"

	"github.com/andrewhowdencom/ore/x/provider/mock"
)

// chatCompletionsPath is the canonical wire path the OpenAI SDK
// targets when the base URL ends in "/v1/" (the SDK's default).
const chatCompletionsPath = "/v1/chat/completions"

// chatCompletionsPathNoV1 is the path the SDK targets when the
// base URL has no "/v1/" suffix (e.g. a raw httptest.Server URL).
// The mock registers both so callers don't need to think about
// which base-URL shape they're using.
const chatCompletionsPathNoV1 = "/chat/completions"

// defaultModel is the model name echoed in the response when the
// request body omits or fails to parse "model". Real OpenAI responses
// always echo the model; for tests that don't care, this keeps the
// response shape valid.
const defaultModel = "gpt-4o"

// defaultStopReason matches the canonical OpenAI finish_reason value
// emitted when the model completes naturally.
const defaultStopReason = "stop"

// ---------------------------------------------------------------------------
// Configuration options.
// ---------------------------------------------------------------------------

// Option configures a Server via the functional-options pattern.
type Option func(*config)

type config struct {
	queue *mock.Queue
}

// WithResponses sets the canned response queue. Successive HTTP requests
// rotate through the supplied [mock.Response] values; a single response
// repeats forever.
func WithResponses(rs ...mock.Response) Option {
	return func(c *config) {
		c.queue = mock.NewQueue(rs...)
	}
}

// ---------------------------------------------------------------------------
// Server.
// ---------------------------------------------------------------------------

// Server is a wire-compatible OpenAI chat-completions streaming mock.
// It owns a [mock.Queue] and an HTTP handler that emits SSE frames for
// each call. Concurrent calls are safe.
type Server struct {
	q       *mock.Queue
	idCount atomic.Uint64
	// handler is the HTTP handler the package installs; constructed
	// once at New() time so Handler() returns the same instance.
	handler http.Handler
}

// New constructs an OpenAI mock server from a list of options. The
// queue must be configured (typically via [WithResponses]); a server
// built with no responses will emit a stream that ends immediately
// with the default finish reason.
func New(opts ...Option) (*Server, error) {
	cfg := &config{}
	for _, opt := range opts {
		opt(cfg)
	}
	if cfg.queue == nil {
		// Default to a single zero-response so callers can construct
		// an empty server without panicking; the produced stream
		// closes immediately.
		cfg.queue = mock.NewQueue()
	}

	s := &Server{q: cfg.queue}
	mux := http.NewServeMux()
	mux.HandleFunc(chatCompletionsPath, s.handleChatCompletions)
	mux.HandleFunc(chatCompletionsPathNoV1, s.handleChatCompletions)
	s.handler = mux
	return s, nil
}

// Handler returns the HTTP handler suitable for `httptest.NewServer`.
// Calling Handler multiple times returns the same instance.
func (s *Server) Handler() http.Handler {
	return s.handler
}

// Start binds a listener on addr and serves until ctx is done or the
// server fails. It blocks. The bound address is printed to stderr in
// the format "mock-server(openai): listening on http://<host>:<port>"
// so callers can discover the URL programmatically.
//
// Start uses net.Listen internally so the bound port (when addr is ":0")
// is known before the first request lands.
func (s *Server) Start(addr string) error {
	srv := &http.Server{Handler: s.handler}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("openai mock: listen %q: %w", addr, err)
	}
	fmt.Fprintf(os.Stderr, "mock-server(openai): listening on http://%s\n", ln.Addr().String())
	return srv.Serve(ln)
}

// ---------------------------------------------------------------------------
// HTTP handler.
// ---------------------------------------------------------------------------

// handleChatCompletions is the HTTP entry point for chat completions
// requests. It reads the model name from the request body and pulls
// the next [mock.Response] from the queue, then writes the SSE
// stream.
func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
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
	w.WriteHeader(http.StatusOK)

	id := s.nextID()
	created := time.Now().Unix()

	if resp.Reasoning != "" {
		s.writeChunk(w, chatChunk{
			ID: id, Object: "chat.completion.chunk",
			Created: created, Model: model,
			Choices: []chatChoice{{
				Index: 0,
				Delta: chatDelta{ReasoningContent: resp.Reasoning},
			}},
		})
	}

	if resp.Text != "" {
		s.writeChunk(w, chatChunk{
			ID: id, Object: "chat.completion.chunk",
			Created: created, Model: model,
			Choices: []chatChoice{{
				Index: 0,
				Delta: chatDelta{Content: resp.Text},
			}},
		})
	}

	for i, tc := range resp.ToolCalls {
		s.writeChunk(w, chatChunk{
			ID: id, Object: "chat.completion.chunk",
			Created: created, Model: model,
			Choices: []chatChoice{{
				Index: 0,
				Delta: chatDelta{
					ToolCalls: []chatToolCallDelta{{
						Index:    i,
						ID:       tc.ID,
						Type:     "function",
						Function: chatToolCallFunction{Name: tc.Name, Arguments: tc.Arguments},
					}},
				},
			}},
		})
	}

	stopReason := resp.StopReason
	if stopReason == "" {
		stopReason = defaultStopReason
	}

	// Final chunk with the stop reason. The OpenAI SDK's read-side
	// (x/wire/openai/openai.go:686) only emits the Usage artifact
	// when len(choices) == 0, so we keep this chunk's choices
	// populated and emit a separate usage chunk below when needed.
	s.writeChunk(w, chatChunk{
		ID: id, Object: "chat.completion.chunk",
		Created: created, Model: model,
		Choices: []chatChoice{{
			Index:        0,
			Delta:        chatDelta{},
			FinishReason: stopReason,
		}},
	})

	// Optional usage chunk. Matches the byte shape in
	// x/wire/openai/openai_test.go:191 — usage is delivered on a
	// dedicated frame with empty choices so the SDK's
	// `len(choices) == 0` branch fires.
	if resp.Usage != nil {
		s.writeChunk(w, chatChunk{
			ID: id, Object: "chat.completion.chunk",
			Created: created, Model: model,
			Choices: []chatChoice{},
			Usage: &chatUsage{
				PromptTokens:     resp.Usage.PromptTokens,
				CompletionTokens: resp.Usage.CompletionTokens,
				TotalTokens:      resp.Usage.TotalTokens,
			},
		})
	}

	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// readModel extracts the model name from the request body. Errors
// fall back to [defaultModel]; the mock does not validate the
// request shape — real OpenAI accepts a wide range of inputs and the
// canned response is the source of truth.
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

// writeChunk marshals a chatChunk and writes it as a single SSE frame.
// The marshaller is called for every event so a per-event outage
// would silently truncate the stream — accepted because every field
// is fully under our control and a marshal failure indicates a
// programmer error, not a runtime condition.
func (s *Server) writeChunk(w http.ResponseWriter, chunk chatChunk) {
	b, err := json.Marshal(chunk)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
}

// nextID generates a deterministic-ish chat-completion id. The
// counter ensures uniqueness across concurrent requests; the
// random suffix matches the visual shape of real OpenAI responses.
func (s *Server) nextID() string {
	var buf [4]byte
	_, _ = rand.Read(buf[:])
	return fmt.Sprintf("chatcmpl-mock-%d-%s", s.idCount.Add(1), hex.EncodeToString(buf[:]))
}

// ---------------------------------------------------------------------------
// Wire types. These mirror the OpenAI chat-completions streaming
// schema; they're emitted verbatim by the mock and parsed by the
// official SDK. Field names use snake_case to match the wire JSON.
// ---------------------------------------------------------------------------

// chatChunk is the streaming delta object emitted on every SSE frame.
// The shape matches x/wire/openai/openai_test.go:142-186 byte-for-byte.
type chatChunk struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []chatChoice `json:"choices"`
	Usage   *chatUsage   `json:"usage,omitempty"`
}

// chatChoice mirrors x/wire/openai.go's `Choices[0]`. ReasoningContent
// lives at the Delta level so it matches the SDK's read path
// (x/wire/openai/openai.go:722 — `delta.reasoning_content`).
type chatChoice struct {
	Index        int       `json:"index"`
	Delta        chatDelta `json:"delta"`
	FinishReason string    `json:"finish_reason,omitempty"`
}

// chatDelta mirrors the streaming delta object. ReasoningContent
// (string) and ToolCalls ([]) are mutually independent — a single
// chunk may set any combination of them.
type chatDelta struct {
	Content          string             `json:"content,omitempty"`
	ReasoningContent string             `json:"reasoning_content,omitempty"`
	ToolCalls        []chatToolCallDelta `json:"tool_calls,omitempty"`
}

// chatToolCallDelta is one tool-call index position in a stream.
// The OpenAI SDK reads these as `delta.ToolCalls` and aggregates
// them by Index. The mock emits one delta per Response.ToolCall.
type chatToolCallDelta struct {
	Index    int                 `json:"index"`
	ID       string              `json:"id"`
	Type     string              `json:"type"`
	Function chatToolCallFunction `json:"function"`
}

// chatToolCallFunction is the function metadata portion of a
// tool-call delta. The OpenAI wire uses this nested shape; the SDK
// reads Arguments verbatim and parses it as JSON when building the
// canonical artifact.ToolCall.
type chatToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// chatUsage mirrors the OpenAI usage block. Cache fields are not
// modeled here; canned responses that need them should add an
// extra-fields approach in a follow-up. For v1, the standard fields
// are sufficient.
type chatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}