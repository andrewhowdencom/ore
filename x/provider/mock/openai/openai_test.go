package openai

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/x/provider/mock"
	xopenai "github.com/andrewhowdencom/ore/x/provider/openai"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readSSE scans a streaming HTTP response into a list of event names
// and their parsed JSON payloads. The OpenAI mock uses a single
// implicit event type (each line is a `data: {...}` chunk), so the
// "event name" is always the literal chunk type. We expose this
// helper because every test in this file wants to assert frame-level
// details.
func readSSE(t *testing.T, body io.Reader) []map[string]any {
	t.Helper()
	var chunks []map[string]any
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			chunks = append(chunks, map[string]any{"_done": true})
			continue
		}
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(payload), &m), "parse %q", payload)
		chunks = append(chunks, m)
	}
	require.NoError(t, scanner.Err())
	return chunks
}

// TestHandler_TextOnly exercises the simplest canned-response shape:
// a single text field. Asserts the exact SSE byte ordering.
func TestHandler_TextOnly(t *testing.T) {
	t.Parallel()

	srv, err := New(WithResponses(mock.Response{
		Text: "Hello, world!",
	}))
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, chatCompletionsPath,
		strings.NewReader(`{"model":"gpt-4o","messages":[]}`))
	srv.handler.ServeHTTP(rec, req)

	assert.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
	assert.Equal(t, http.StatusOK, rec.Code)

	chunks := readSSE(t, rec.Body)
	require.Len(t, chunks, 3, "text chunk + final chunk + DONE")

	// Chunk 0: text delta.
	c0 := chunks[0]
	assert.Equal(t, "chat.completion.chunk", c0["object"])
	assert.Equal(t, "gpt-4o", c0["model"])
	choices, ok := c0["choices"].([]any)
	require.True(t, ok, "choices missing in chunk 0")
	require.Len(t, choices, 1)
	choice0 := choices[0].(map[string]any)
	delta := choice0["delta"].(map[string]any)
	assert.Equal(t, "Hello, world!", delta["content"])
	_, hasFinish := choice0["finish_reason"]
	assert.False(t, hasFinish, "intermediate chunk should not set finish_reason")

	// Chunk 1: final — finish_reason set, empty delta, no usage.
	c1 := chunks[1]
	choices = c1["choices"].([]any)
	require.Len(t, choices, 1)
	choice1 := choices[0].(map[string]any)
	assert.Equal(t, "stop", choice1["finish_reason"])
	assert.Empty(t, choice1["delta"].(map[string]any))

	// Chunk 2: terminator.
	assert.True(t, chunks[2]["_done"].(bool))
}

// TestHandler_Usage exercises the usage-emission path: when Response.Usage
// is set, a dedicated usage chunk follows the final-chunk-with-stop.
//
// The SDK only emits a [artifact.Usage] when it sees a chunk with
// `len(Choices) == 0` AND a non-zero TotalTokens (x/wire/openai/openai.go:686).
// The mock emits usage on a frame of its own so this branch fires.
func TestHandler_Usage(t *testing.T) {
	t.Parallel()

	srv, err := New(WithResponses(mock.Response{
		Text: "counted",
		Usage: &mock.Usage{
			PromptTokens:     12,
			CompletionTokens: 7,
			TotalTokens:      19,
		},
	}))
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, chatCompletionsPath,
		strings.NewReader(`{}`)))

	chunks := readSSE(t, rec.Body)
	require.Len(t, chunks, 4, "text + stop + usage + DONE")

	// chunk[1] is the stop-reason chunk.
	stop := chunks[1]
	choices := stop["choices"].([]any)
	require.Len(t, choices, 1)
	stopChoice := choices[0].(map[string]any)
	assert.Equal(t, "stop", stopChoice["finish_reason"])
	assert.Empty(t, stopChoice["delta"].(map[string]any), "stop chunk delta must be empty")
	_, hasUsage := stop["usage"]
	assert.False(t, hasUsage, "stop chunk must not carry usage")

	// chunk[2] is the dedicated usage chunk: empty choices, usage populated.
	usageChunk := chunks[2]
	assert.Empty(t, usageChunk["choices"].([]any), "usage chunk must have empty choices")
	usage, ok := usageChunk["usage"].(map[string]any)
	require.True(t, ok, "usage must be present")
	assert.EqualValues(t, 12, usage["prompt_tokens"])
	assert.EqualValues(t, 7, usage["completion_tokens"])
	assert.EqualValues(t, 19, usage["total_tokens"])
}

// TestHandler_Reasoning exercises the reasoning-delivery path. The
// OpenAI SDK reads `delta.reasoning_content` (see x/wire/openai/openai.go:722).
func TestHandler_Reasoning(t *testing.T) {
	t.Parallel()

	srv, err := New(WithResponses(mock.Response{
		Reasoning: "I am thinking...",
		Text:      "answer",
	}))
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, chatCompletionsPath,
		strings.NewReader(`{}`)))

	chunks := readSSE(t, rec.Body)
	require.Len(t, chunks, 4, "reasoning + text + final + DONE")

	reasoning := chunks[0]["choices"].([]any)[0].(map[string]any)["delta"].(map[string]any)["reasoning_content"]
	assert.Equal(t, "I am thinking...", reasoning)
}

// TestHandler_ToolCalls verifies that each [mock.ToolCall] becomes one
// streaming chunk with a properly-shaped delta.tool_calls array.
func TestHandler_ToolCalls(t *testing.T) {
	t.Parallel()

	srv, err := New(WithResponses(mock.Response{
		Text: "tooling",
		ToolCalls: []mock.ToolCall{
			{ID: "call_1", Name: "search", Arguments: `{"q":"x"}`},
			{ID: "call_2", Name: "fetch", Arguments: `{"url":"y"}`},
		},
	}))
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, chatCompletionsPath,
		strings.NewReader(`{}`)))

	chunks := readSSE(t, rec.Body)
	// Expect: text + tool1 + tool2 + final + DONE = 5.
	require.Len(t, chunks, 5)

	tcChunk := chunks[1]
	choice := tcChunk["choices"].([]any)[0].(map[string]any)
	deltas := choice["delta"].(map[string]any)["tool_calls"].([]any)
	require.Len(t, deltas, 1)
	d0 := deltas[0].(map[string]any)
	assert.EqualValues(t, 0, d0["index"])
	assert.Equal(t, "call_1", d0["id"])
	assert.Equal(t, "function", d0["type"])
	fn := d0["function"].(map[string]any)
	assert.Equal(t, "search", fn["name"])
	assert.Equal(t, `{"q":"x"}`, fn["arguments"])
}

// TestHandler_QueueRotation exercises the queue's round-robin across
// consecutive HTTP requests to the same handler.
func TestHandler_QueueRotation(t *testing.T) {
	t.Parallel()

	srv, err := New(WithResponses(
		mock.Response{Text: "first"},
		mock.Response{Text: "second"},
	))
	require.NoError(t, err)

	get := func() string {
		rec := httptest.NewRecorder()
		srv.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost,
			chatCompletionsPath, strings.NewReader(`{}`)))
		chunks := readSSE(t, rec.Body)
		return chunks[0]["choices"].([]any)[0].(map[string]any)["delta"].(map[string]any)["content"].(string)
	}

	assert.Equal(t, "first", get())
	assert.Equal(t, "second", get())
	assert.Equal(t, "first", get(), "queue should wrap")
}

// TestHandler_EmptyResponse produces a stream with only the final
// chunk + DONE — no content, no reasoning, no tool calls. The real
// OpenAI SDK closes cleanly on such a stream.
func TestHandler_EmptyResponse(t *testing.T) {
	t.Parallel()

	srv, err := New(WithResponses(mock.Response{}))
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost,
		chatCompletionsPath, strings.NewReader(`{}`)))

	chunks := readSSE(t, rec.Body)
	require.Len(t, chunks, 2, "final + DONE only")
	choice := chunks[0]["choices"].([]any)[0].(map[string]any)
	assert.Equal(t, "stop", choice["finish_reason"])
}

// TestHandler_ConcurrentSafe asserts that N goroutines hitting the
// handler concurrently each get a well-formed response. Run with
// `-race` to detect concurrent map writes (we use no maps, but the
// counter is atomic).
func TestHandler_ConcurrentSafe(t *testing.T) {
	t.Parallel()

	srv, err := New(WithResponses(mock.Response{Text: "ok"}))
	require.NoError(t, err)

	const goroutines = 16
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			srv.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost,
				chatCompletionsPath, strings.NewReader(`{}`)))
			chunks := readSSE(t, rec.Body)
			if len(chunks) != 3 {
				t.Errorf("expected 3 chunks, got %d", len(chunks))
			}
		}()
	}
	wg.Wait()
}

// TestIntegration_RealProvider drives the real [xopenai.Provider]
// against the mock and asserts the artifacts it emits. This is the
// end-to-end check that proves the wire format is correct: if the
// SDK stops accepting our SSE frames, this test breaks first.
func TestIntegration_RealProvider(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	srv, err := New(WithResponses(
		mock.Response{Text: "hello"},
		mock.Response{Text: "world", Usage: &mock.Usage{
			PromptTokens:     5,
			CompletionTokens: 5,
			TotalTokens:      10,
		}},
	))
	require.NoError(t, err)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	prov, err := xopenai.New(
		xopenai.WithAPIKey("test-key"),
		xopenai.WithBaseURL(ts.URL),
	)
	require.NoError(t, err)

	collect := func(spec models.Spec) []artifact.Artifact {
		state := ledger.NewThread()
		state.Append(ledger.RoleUser, artifact.Text{Content: "ping"})
		ch := make(chan artifact.Artifact, 64)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := prov.Invoke(ctx, state, spec, ch)
		close(ch)
		require.NoError(t, err)
		var out []artifact.Artifact
		for a := range ch {
			out = append(out, a)
		}
		return out
	}

	// First turn: text-only, no usage.
	arts := collect(models.Spec{Name: "gpt-4o"})
	var texts []string
	var stopReasons []string
	var hasUsage bool
	for _, a := range arts {
		switch v := a.(type) {
		case artifact.TextDelta:
			texts = append(texts, v.Content)
		case artifact.StopReason:
			stopReasons = append(stopReasons, string(v.Reason))
		case artifact.Usage:
			hasUsage = true
		}
	}
	assert.Equal(t, []string{"hello"}, texts, "first turn should yield one text delta")
	require.NotEmpty(t, stopReasons, "first turn should emit a stop reason")
	assert.Equal(t, "stop", stopReasons[0])
	assert.False(t, hasUsage, "first turn had no usage")

	// Second turn: text + usage.
	arts = collect(models.Spec{Name: "gpt-4o"})
	texts = texts[:0]
	stopReasons = stopReasons[:0]
	hasUsage = false
	var usage artifact.Usage
	for _, a := range arts {
		switch v := a.(type) {
		case artifact.TextDelta:
			texts = append(texts, v.Content)
		case artifact.StopReason:
			stopReasons = append(stopReasons, string(v.Reason))
		case artifact.Usage:
			hasUsage = true
			usage = v
		}
	}
	assert.Equal(t, []string{"world"}, texts)
	require.NotEmpty(t, stopReasons)
	assert.Equal(t, "stop", stopReasons[0])
	assert.True(t, hasUsage, "second turn must include a usage artifact")
	assert.EqualValues(t, 5, usage.PromptTokens)
	assert.EqualValues(t, 5, usage.CompletionTokens)
	assert.EqualValues(t, 10, usage.TotalTokens)
}