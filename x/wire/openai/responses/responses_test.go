package responses

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/tool"
	"github.com/andrewhowdencom/ore/x/provider/retry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvokeSerializesRequestAndTranslatesStream(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer test", r.Header.Get("Authorization"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&captured))
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, strings.Join([]string{
			`data: {"type":"response.output_text.delta","delta":"hello"}`,
			`data: {"type":"response.reasoning_summary_text.delta","delta":"thinking"}`,
			`data: {"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","call_id":"call-1","name":"lookup","arguments":""}}`,
			`data: {"type":"response.function_call_arguments.delta","output_index":1,"call_id":"call-1","delta":"{\"q\":"}`,
			`data: {"type":"response.function_call_arguments.delta","output_index":1,"call_id":"call-1","delta":"\"ore\"}"}`,
			`data: {"type":"response.output_item.done","item":{"type":"reasoning","encrypted_content":"opaque"}}`,
			`data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15,"input_tokens_details":{"cached_tokens":3},"output_tokens_details":{"reasoning_tokens":2}}}}`,
			`data: [DONE]`, "",
		}, "\n\n"))
	}))
	defer server.Close()

	thread := ledger.NewThread()
	thread.Append(ledger.RoleSystem, artifact.Text{Content: "be useful"})
	thread.Append(ledger.RoleUser, artifact.Text{Content: "hi"}, artifact.Image{URL: "https://example.test/image.png"})
	thread.Append(ledger.RoleAssistant,
		artifact.Text{Content: "working"},
		artifact.ToolCall{ID: "old-call", Name: "lookup", Arguments: `{"q":"old"}`},
		artifact.ReasoningSignature{Provider: "openai", SubKind: "encrypted", Data: "old-opaque"},
	)
	thread.Append(ledger.RoleTool, artifact.ToolResult{ToolCallID: "old-call", Content: `{"ok":true}`})

	wire, err := New(
		WithEndpoint(server.URL),
		WithRequestEditor(func(_ context.Context, req *http.Request) error {
			req.Header.Set("Authorization", "Bearer test")
			return nil
		}),
	)
	require.NoError(t, err)
	ch := make(chan artifact.Artifact, 16)
	err = wire.Invoke(context.Background(), thread, models.Spec{Name: "gpt-test", ThinkingLevel: models.ThinkingLevelHigh}, ch,
		provider.WithTools([]tool.Tool{{Name: "lookup", Description: "look up", Schema: map[string]any{"type": "object"}}}),
		WithSessionID("thread-1"),
	)
	require.NoError(t, err)
	close(ch)
	var got []artifact.Artifact
	for item := range ch {
		got = append(got, item)
	}

	assert.Equal(t, "gpt-test", captured["model"])
	assert.Equal(t, "be useful", captured["instructions"])
	assert.Equal(t, false, captured["store"])
	assert.Equal(t, true, captured["stream"])
	assert.Equal(t, "thread-1", captured["prompt_cache_key"])
	assert.Len(t, captured["tools"], 1)
	assert.Len(t, captured["input"], 5)
	require.Len(t, got, 8)
	assert.Equal(t, artifact.TextDelta{Content: "hello"}, got[0])
	assert.Equal(t, artifact.ReasoningDelta{Content: "thinking"}, got[1])
	assert.Equal(t, artifact.ToolCallDelta{Index: 1, ID: "call-1", Name: "lookup"}, got[2])
	assert.Equal(t, artifact.ReasoningSignature{Provider: "openai", SubKind: "encrypted", Data: "opaque"}, got[5])
	assert.Equal(t, artifact.StopReason{Reason: artifact.StopReasonToolUse}, got[6])
	assert.Equal(t, artifact.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15, CacheReadTokens: 3, ThinkingTokens: intPtr(2)}, got[7])
}

func TestInvokeHTTPErrorSupportsRetryClassifier(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "1")
		http.Error(w, "busy", http.StatusTooManyRequests)
	}))
	defer server.Close()
	wire, err := New(WithEndpoint(server.URL))
	require.NoError(t, err)
	err = wire.Invoke(context.Background(), ledger.NewThread(), models.Spec{Name: "gpt-test"}, make(chan artifact.Artifact, 1))
	require.Error(t, err)
	var httpErr retry.HTTPError
	require.True(t, errors.As(err, &httpErr))
	assert.Equal(t, http.StatusTooManyRequests, httpErr.StatusCode())
}

func TestConsumeSSERejectsMalformedEvent(t *testing.T) {
	err := consumeSSE(context.Background(), strings.NewReader("data: {nope}\n\n"), make(chan artifact.Artifact, 1))
	require.ErrorContains(t, err, "decode SSE event")
}

func TestConsumeSSERequiresTerminalEvent(t *testing.T) {
	err := consumeSSE(context.Background(), strings.NewReader("data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n"), make(chan artifact.Artifact, 1))
	require.ErrorContains(t, err, "stream closed before response.completed")
}

func TestConsumeSSEAcceptsTopLevelResponseDone(t *testing.T) {
	ch := make(chan artifact.Artifact, 2)
	err := consumeSSE(context.Background(), strings.NewReader("data: {\"type\":\"response.done\",\"status\":\"completed\",\"usage\":{\"input_tokens\":1,\"output_tokens\":2,\"total_tokens\":3}}\n\n"), ch)
	require.NoError(t, err)
	assert.Equal(t, artifact.StopReason{Reason: artifact.StopReasonStop}, <-ch)
	assert.Equal(t, artifact.Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3}, <-ch)
}

func TestConsumeSSEPreservesFunctionCallIDAcrossArgumentDeltas(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_123","call_id":"call_456","name":"lookup","arguments":""}}`,
		`data: {"type":"response.function_call_arguments.delta","item_id":"fc_123","output_index":0,"delta":"{\"q\":"}`,
		`data: {"type":"response.function_call_arguments.delta","item_id":"fc_123","output_index":0,"delta":"\"ore\"}"}`,
		`data: {"type":"response.completed","response":{"status":"completed"}}`,
		"",
	}, "\n\n")
	ch := make(chan artifact.Artifact, 4)

	require.NoError(t, consumeSSE(context.Background(), strings.NewReader(stream), ch))
	close(ch)

	var accumulated artifact.Artifact
	for item := range ch {
		if delta, ok := item.(artifact.ToolCallDelta); ok {
			accumulated = delta.MergeInto(accumulated)
		}
	}
	assert.Equal(t, artifact.ToolCall{
		ID:        "call_456",
		Name:      "lookup",
		Arguments: `{"q":"ore"}`,
	}, accumulated)
}

func TestInvokeReturnsAfterTerminalEventWithoutWaitingForEOF(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()

	wire, err := New(WithEndpoint(server.URL))
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err = wire.Invoke(ctx, ledger.NewThread(), models.Spec{Name: "gpt-test"}, make(chan artifact.Artifact, 1))
	require.NoError(t, err)
}

func intPtr(value int) *int { return &value }
