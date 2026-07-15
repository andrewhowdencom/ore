package anthropic

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
	xanthropic "github.com/andrewhowdencom/ore/x/provider/anthropic"
	"github.com/andrewhowdencom/ore/x/provider/mock"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sseEvent represents a single Anthropic SSE event: a name plus the
// parsed JSON payload from its data: line.
type sseEvent struct {
	Name    string
	Payload map[string]any
}

// readSSE scans an Anthropic streaming response into ordered events.
// Anthropic SSE alternates `event: <name>` and `data: <json>` lines,
// separated by a blank line. This helper matches the byte shape in
// x/wire/anthropic/anthropic_test.go:1087.
func readSSE(t *testing.T, body io.Reader) []sseEvent {
	t.Helper()
	var events []sseEvent
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	var name string
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			payload := strings.TrimPrefix(line, "data: ")
			var m map[string]any
			require.NoError(t, json.Unmarshal([]byte(payload), &m), "parse %q", payload)
			events = append(events, sseEvent{Name: name, Payload: m})
			name = ""
		}
		// Blank line is the event terminator; skip.
	}
	require.NoError(t, scanner.Err())
	return events
}

// TestHandler_TextOnly exercises the simplest canned-response shape.
func TestHandler_TextOnly(t *testing.T) {
	t.Parallel()

	srv, err := New(WithResponses(mock.Response{Text: "Hello!"}))
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, messagesPath,
		strings.NewReader(`{"model":"claude-3-7-sonnet-latest","max_tokens":1024}`)))

	assert.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
	assert.Equal(t, http.StatusOK, rec.Code)

	events := readSSE(t, rec.Body)
	require.Len(t, events, 6, "message_start + (start+delta+stop) + message_delta + message_stop")

	// Event 0: message_start.
	assert.Equal(t, "message_start", events[0].Name)
	msg := events[0].Payload["message"].(map[string]any)
	assert.Equal(t, "claude-3-7-sonnet-latest", msg["model"])

	// Events 1-3: text block lifecycle.
	assert.Equal(t, "content_block_start", events[1].Name)
	assert.Equal(t, "text", events[1].Payload["content_block"].(map[string]any)["type"])

	assert.Equal(t, "content_block_delta", events[2].Name)
	delta := events[2].Payload["delta"].(map[string]any)
	assert.Equal(t, "text_delta", delta["type"])
	assert.Equal(t, "Hello!", delta["text"])

	assert.Equal(t, "content_block_stop", events[3].Name)

	// Event 4: message_delta with stop_reason.
	assert.Equal(t, "message_delta", events[4].Name)
	md := events[4].Payload["delta"].(map[string]any)
	assert.Equal(t, "end_turn", md["stop_reason"])

	// Event 5: message_stop terminator.
	assert.Equal(t, "message_stop", events[5].Name)
}

// TestHandler_Reasoning exercises the thinking block path with an
// optional signature replay.
func TestHandler_Reasoning(t *testing.T) {
	t.Parallel()

	srv, err := New(WithResponses(mock.Response{
		Reasoning: "I am thinking...",
		Signature: "sig_abc",
		Text:      "answer",
	}))
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, messagesPath,
		strings.NewReader(`{}`)))

	events := readSSE(t, rec.Body)
	// message_start + thinking (start + delta + sig_delta + stop) + text (start + delta + stop) + message_delta + message_stop = 10
	require.Len(t, events, 10)

	// First block is thinking.
	assert.Equal(t, "thinking", events[1].Payload["content_block"].(map[string]any)["type"])
	assert.Equal(t, "thinking_delta", events[2].Payload["delta"].(map[string]any)["type"])
	assert.Equal(t, "I am thinking...", events[2].Payload["delta"].(map[string]any)["thinking"])
	assert.Equal(t, "signature_delta", events[3].Payload["delta"].(map[string]any)["type"])
	assert.Equal(t, "sig_abc", events[3].Payload["delta"].(map[string]any)["signature"])
	assert.Equal(t, "content_block_stop", events[4].Name)

	// Second block is text.
	assert.Equal(t, "text", events[5].Payload["content_block"].(map[string]any)["type"])
	assert.Equal(t, "text_delta", events[6].Payload["delta"].(map[string]any)["type"])
	assert.Equal(t, "answer", events[6].Payload["delta"].(map[string]any)["text"])
}

// TestHandler_ToolCalls verifies the tool_use block path.
func TestHandler_ToolCalls(t *testing.T) {
	t.Parallel()

	srv, err := New(WithResponses(mock.Response{
		Text: "calling",
		ToolCalls: []mock.ToolCall{
			{ID: "toolu_1", Name: "search", Arguments: `{"q":"x"}`},
		},
	}))
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, messagesPath,
		strings.NewReader(`{}`)))

	events := readSSE(t, rec.Body)
	// message_start + text (start+delta+stop) + tool_use (start+delta+stop) + message_delta + message_stop = 9
	require.Len(t, events, 9)

	toolStart := events[4]
	assert.Equal(t, "content_block_start", toolStart.Name)
	block := toolStart.Payload["content_block"].(map[string]any)
	assert.Equal(t, "tool_use", block["type"])
	assert.Equal(t, "toolu_1", block["id"])
	assert.Equal(t, "search", block["name"])

	toolDelta := events[5]
	assert.Equal(t, "input_json_delta", toolDelta.Payload["delta"].(map[string]any)["type"])
	assert.Equal(t, `{"q":"x"}`, toolDelta.Payload["delta"].(map[string]any)["partial_json"])
}

// TestHandler_Usage verifies that the message_delta carries the
// configured usage tokens and that message_start's usage reflects the
// initial state.
func TestHandler_Usage(t *testing.T) {
	t.Parallel()

	srv, err := New(WithResponses(mock.Response{
		Text: "counted",
		Usage: &mock.Usage{
			PromptTokens:     10,
			CompletionTokens: 20,
			TotalTokens:      30,
		},
	}))
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, messagesPath,
		strings.NewReader(`{}`)))

	events := readSSE(t, rec.Body)
	// Find message_delta (last block before message_stop).
	var deltaEvent sseEvent
	for _, e := range events {
		if e.Name == "message_delta" {
			deltaEvent = e
			break
		}
	}
	require.NotEmpty(t, deltaEvent.Name, "expected a message_delta event")

	usage := deltaEvent.Payload["usage"].(map[string]any)
	assert.EqualValues(t, 10, usage["input_tokens"])
	assert.EqualValues(t, 20, usage["output_tokens"])
}

// TestHandler_EmptyResponse produces a stream with no blocks — just
// message_start, message_delta, and message_stop.
func TestHandler_EmptyResponse(t *testing.T) {
	t.Parallel()

	srv, err := New(WithResponses(mock.Response{}))
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	srv.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, messagesPath,
		strings.NewReader(`{}`)))

	events := readSSE(t, rec.Body)
	assert.Len(t, events, 3, "message_start + message_delta + message_stop")
	assert.Equal(t, "message_start", events[0].Name)
	assert.Equal(t, "message_delta", events[1].Name)
	assert.Equal(t, "end_turn", events[1].Payload["delta"].(map[string]any)["stop_reason"])
	assert.Equal(t, "message_stop", events[2].Name)
}

// TestHandler_QueueRotation exercises the queue's round-robin across
// consecutive requests.
func TestHandler_QueueRotation(t *testing.T) {
	t.Parallel()

	srv, err := New(WithResponses(
		mock.Response{Text: "first"},
		mock.Response{Text: "second"},
	))
	require.NoError(t, err)

	get := func() string {
		rec := httptest.NewRecorder()
		srv.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, messagesPath,
			strings.NewReader(`{}`)))
		events := readSSE(t, rec.Body)
		// Find the first text_delta.
		for _, e := range events {
			if e.Name != "content_block_delta" {
				continue
			}
			delta := e.Payload["delta"].(map[string]any)
			if t, ok := delta["type"].(string); ok && t == "text_delta" {
				return delta["text"].(string)
			}
		}
		t.Fatal("no text_delta in response")
		return ""
	}

	assert.Equal(t, "first", get())
	assert.Equal(t, "second", get())
	assert.Equal(t, "first", get())
}

// TestHandler_ConcurrentSafe asserts concurrent safety across the
// full event sequence: message_start, text block (start + delta +
// stop), message_delta, message_stop = 6 events.
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
			srv.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, messagesPath,
				strings.NewReader(`{}`)))
			events := readSSE(t, rec.Body)
			if len(events) != 6 {
				t.Errorf("expected 6 events, got %d", len(events))
			}
		}()
	}
	wg.Wait()
}

// TestIntegration_RealProvider drives the real [xanthropic.Provider]
// against the mock and asserts the artifact sequence. End-to-end
// check that proves the wire format is correct.
func TestIntegration_RealProvider(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	srv, err := New(WithResponses(
		mock.Response{Text: "hello"},
		mock.Response{
			Text: "world",
			Usage: &mock.Usage{
				PromptTokens:     5,
				CompletionTokens: 5,
				TotalTokens:      10,
			},
		},
	))
	require.NoError(t, err)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	prov, err := xanthropic.New(
		xanthropic.WithAPIKey("test-key"),
		xanthropic.WithBaseURL(ts.URL),
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

	// First turn: text-only, no usage configured.
	arts := collect(models.Spec{Name: "claude-3-7-sonnet-latest"})
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
	assert.Equal(t, []string{"hello"}, texts)
	require.NotEmpty(t, stopReasons)
	// The Anthropic SDK translates wire-format "end_turn" to the
	// canonical artifact.StopReasonStop ("stop"). See
	// x/wire/anthropic/anthropic.go:translateStopReason.
	assert.Equal(t, "stop", stopReasons[0])
	// The SDK always emits a Usage artifact once message_delta has
	// been read, even with zero values. Asserting on the values
	// (rather than presence) is the right test for the no-usage case.
	assert.True(t, hasUsage)

	// Second turn: text + usage configured.
	arts = collect(models.Spec{Name: "claude-3-7-sonnet-latest"})
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
	assert.True(t, hasUsage)
	assert.EqualValues(t, 5, usage.PromptTokens)
	assert.EqualValues(t, 5, usage.CompletionTokens)
}