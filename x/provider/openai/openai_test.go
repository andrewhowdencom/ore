package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/state"
	toolpkg "github.com/andrewhowdencom/ore/tool"
	xtool "github.com/andrewhowdencom/ore/x/tool"
	"go.opentelemetry.io/otel/trace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTransport is an http.RoundTripper that returns a canned response and
// optionally captures the outgoing request for inspection.
type mockTransport struct {
	response *http.Response
	request  *http.Request
	err      error
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	m.request = req
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}

// concurrentMockTransport returns a fresh response for each request,
// making it safe for concurrent use.
type concurrentMockTransport struct {
	responseBody string
}

func (m *concurrentMockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(m.responseBody)),
	}, nil
}

// recordedRequest captures an outgoing HTTP request and its body bytes
// for later inspection.
type recordedRequest struct {
	request *http.Request
	body    []byte
}

// recordingMockTransport is safe for concurrent use and captures every
// outgoing request together with its body in a mutex-protected slice.
type recordingMockTransport struct {
	mu           sync.Mutex
	requests     []recordedRequest
	responseBody string
	err          error
}

func (m *recordingMockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)

	m.mu.Lock()
	m.requests = append(m.requests, recordedRequest{request: req, body: body})
	m.mu.Unlock()

	if m.err != nil {
		return nil, m.err
	}
	return &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(m.responseBody)),
	}, nil
}

func (m *recordingMockTransport) Requests() []recordedRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]recordedRequest, len(m.requests))
	copy(out, m.requests)
	return out
}

func mockClient(transport *mockTransport) *http.Client {
	return &http.Client{Transport: transport}
}

func mockResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func mockResponseSSE(body string) *http.Response {
	return &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// errorReader is an io.Reader that returns preloaded data followed by a fixed
// error, simulating a network failure mid-stream.
type errorReader struct {
	data []byte
	pos  int
	err  error
}

func (r *errorReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, r.err
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

func simpleSSE(content string) string {
	return fmt.Sprintf("data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4\",\"choices\":[{\"index\":0,\"delta\":{\"content\":%q},\"finish_reason\":null}]}\n\ndata: [DONE]\n\n", content)
}

func reasoningSSE(content, reasoning string) string {
	return fmt.Sprintf("data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"o3-mini\",\"choices\":[{\"index\":0,\"delta\":{\"content\":%q,\"reasoning_content\":%q},\"finish_reason\":null}]}\n\ndata: [DONE]\n\n", content, reasoning)
}

func reasoningOnlySSE(parts ...string) string {
	var sb strings.Builder
	for i, part := range parts {
		sb.WriteString(fmt.Sprintf("data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"created\":%d,\"model\":\"o3-mini\",\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":%q},\"finish_reason\":null}]}\n\n", i+1, part))
	}
	sb.WriteString("data: [DONE]\n\n")
	return sb.String()
}

func emptyChoicesSSE() string {
	return "data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"choices\":[]}\n\ndata: [DONE]\n\n"
}

func usageSSE(prompt, completion, total int64) string {
	return fmt.Sprintf("data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4\",\"choices\":[],\"usage\":{\"prompt_tokens\":%d,\"completion_tokens\":%d,\"total_tokens\":%d}}\n\ndata: [DONE]\n\n", prompt, completion, total)
}

func textWithUsageSSE(content string, promptTokens, completionTokens, totalTokens int64) string {
	return fmt.Sprintf(
		"data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4\",\"choices\":[{\"index\":0,\"delta\":{\"content\":%q},\"finish_reason\":null}]}\n\n"+
			"data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4\",\"choices\":[],\"usage\":{\"prompt_tokens\":%d,\"completion_tokens\":%d,\"total_tokens\":%d}}\n\ndata: [DONE]\n\n",
		content, promptTokens, completionTokens, totalTokens,
	)
}

func drainArtifacts(ch chan artifact.Artifact) []artifact.Artifact {
	close(ch)
	var artifacts []artifact.Artifact
	for art := range ch {
		artifacts = append(artifacts, art)
	}
	return artifacts
}

func TestProviderInvoke_Success(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(simpleSSE("Hello, world!")),
	}

	p, err := New(WithAPIKey("test-key"), WithModel("gpt-4"), WithHTTPClient(mockClient(transport)))
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	err = p.Invoke(t.Context(), mem, ch)
	require.NoError(t, err)

	artifacts := drainArtifacts(ch)
	// Only delta emitted; no complete artifact at end.
	require.Len(t, artifacts, 1)
	assert.Equal(t, "text_delta", artifacts[0].Kind())
	assert.Equal(t, "Hello, world!", artifacts[0].(artifact.TextDelta).Content)
}

func TestProviderInvoke_HTTPError(t *testing.T) {
	transport := &mockTransport{
		response: mockResponse(401, `{"error":{"message":"invalid key","type":"invalid_request_error"}}`),
	}

	p, err := New(WithAPIKey("bad-key"), WithModel("gpt-4"), WithHTTPClient(mockClient(transport)))
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	err = p.Invoke(t.Context(), mem, ch)
	require.Error(t, err)
}

func TestProviderInvoke_EmptyChoices(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(emptyChoicesSSE()),
	}

	p, err := New(WithAPIKey("test-key"), WithModel("gpt-4"), WithHTTPClient(mockClient(transport)))
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	err = p.Invoke(t.Context(), mem, ch)
	require.NoError(t, err)

	artifacts := drainArtifacts(ch)
	assert.Empty(t, artifacts)
}

func TestProviderInvoke_UsageChunk(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(usageSSE(10, 5, 15)),
	}

	p, err := New(WithAPIKey("test-key"), WithModel("gpt-4"), WithHTTPClient(mockClient(transport)))
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	err = p.Invoke(t.Context(), mem, ch)
	require.NoError(t, err)

	artifacts := drainArtifacts(ch)
	require.Len(t, artifacts, 1)
	assert.Equal(t, "usage", artifacts[0].Kind())
	u, ok := artifacts[0].(artifact.Usage)
	require.True(t, ok)
	assert.Equal(t, 10, u.PromptTokens)
	assert.Equal(t, 5, u.CompletionTokens)
	assert.Equal(t, 15, u.TotalTokens)
}

func TestProviderInvoke_MultipleTextArtifacts(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(simpleSSE("ok")),
	}

	p, err := New(WithAPIKey("test-key"), WithModel("gpt-4"), WithHTTPClient(mockClient(transport)))
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser,
		artifact.Text{Content: "line1"},
		artifact.Text{Content: "line2"},
	)

	ch := make(chan artifact.Artifact, 10)
	_ = p.Invoke(t.Context(), mem, ch)
	close(ch)
	for range ch {
	}

	require.NotNil(t, transport.request)
	body, _ := io.ReadAll(transport.request.Body)
	assert.Contains(t, string(body), "line1\\nline2")
}

func TestProviderInvoke_NonTextArtifactsSkipped(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(simpleSSE("ok")),
	}

	p, err := New(WithAPIKey("test-key"), WithModel("gpt-4"), WithHTTPClient(mockClient(transport)))
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser,
		artifact.Text{Content: "hello"},
		artifact.ToolCall{Name: "foo", Arguments: "{}"},
		artifact.Image{URL: "http://example.com/img.png"},
	)

	ch := make(chan artifact.Artifact, 10)
	_ = p.Invoke(t.Context(), mem, ch)
	close(ch)
	for range ch {
	}

	require.NotNil(t, transport.request)
	body, _ := io.ReadAll(transport.request.Body)
	var reqBody map[string]any
	require.NoError(t, json.Unmarshal(body, &reqBody))

	msgs, ok := reqBody["messages"].([]any)
	require.True(t, ok)
	require.Len(t, msgs, 1)

	msg, ok := msgs[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "hello", msg["content"])
}

func TestProviderInvoke_EmptyState(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(simpleSSE("ok")),
	}

	p, err := New(WithAPIKey("test-key"), WithModel("gpt-4"), WithHTTPClient(mockClient(transport)))
	require.NoError(t, err)
	mem := &state.Buffer{}

	ch := make(chan artifact.Artifact, 10)
	_ = p.Invoke(t.Context(), mem, ch)
	close(ch)
	for range ch {
	}

	require.NotNil(t, transport.request)
	body, _ := io.ReadAll(transport.request.Body)
	var reqBody map[string]any
	require.NoError(t, json.Unmarshal(body, &reqBody))

	msgs, ok := reqBody["messages"].([]any)
	require.True(t, ok)
	assert.Empty(t, msgs)
}

func TestProviderInvoke_MultipleChoices(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(simpleSSE("first")),
	}

	p, err := New(WithAPIKey("test-key"), WithModel("gpt-4"), WithHTTPClient(mockClient(transport)))
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	err = p.Invoke(t.Context(), mem, ch)
	require.NoError(t, err)

	artifacts := drainArtifacts(ch)
	// Only delta emitted; no complete artifact at end.
	require.Len(t, artifacts, 1)
	delta, ok := artifacts[0].(artifact.TextDelta)
	require.True(t, ok)
	assert.Equal(t, "first", delta.Content)
}

func TestProviderInvoke_MalformedJSON(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE("data: {\"invalid\n\ndata: [DONE]\n\n"),
	}

	p, err := New(WithAPIKey("test-key"), WithModel("gpt-4"), WithHTTPClient(mockClient(transport)))
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	err = p.Invoke(t.Context(), mem, ch)
	require.Error(t, err)
}

func TestProviderInvoke_ContextCancellation(t *testing.T) {
	transport := &mockTransport{
		err: context.Canceled,
	}

	p, err := New(WithAPIKey("test-key"), WithModel("gpt-4"), WithHTTPClient(mockClient(transport)))
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ch := make(chan artifact.Artifact, 10)
	err = p.Invoke(ctx, mem, ch)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestProviderInvoke_CustomClient(t *testing.T) {
	wantErr := errors.New("custom transport error")
	transport := &mockTransport{
		err: wantErr,
	}

	p, err := New(WithAPIKey("test-key"), WithModel("gpt-4"), WithHTTPClient(mockClient(transport)))
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	err = p.Invoke(t.Context(), mem, ch)
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
}

func TestProviderInvoke_WithReasoning(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(reasoningSSE("Hello, world!", "Let me analyze this...")),
	}

	p, err := New(WithAPIKey("test-key"), WithModel("o3-mini"), WithHTTPClient(mockClient(transport)))
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	err = p.Invoke(t.Context(), mem, ch)
	require.NoError(t, err)

	artifacts := drainArtifacts(ch)
	// text_delta, reasoning_delta — only deltas emitted.
	require.Len(t, artifacts, 2)
	assert.Equal(t, "text_delta", artifacts[0].Kind())
	assert.Equal(t, "Hello, world!", artifacts[0].(artifact.TextDelta).Content)
	assert.Equal(t, "reasoning_delta", artifacts[1].Kind())
	assert.Equal(t, "Let me analyze this...", artifacts[1].(artifact.ReasoningDelta).Content)
}

func TestProviderInvoke_EmptyReasoning(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE("data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"o3-mini\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hello, world!\",\"reasoning_content\":\"\"},\"finish_reason\":null}]}\n\ndata: [DONE]\n\n"),
	}

	p, err := New(WithAPIKey("test-key"), WithModel("o3-mini"), WithHTTPClient(mockClient(transport)))
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	err = p.Invoke(t.Context(), mem, ch)
	require.NoError(t, err)

	artifacts := drainArtifacts(ch)
	// text_delta only — empty reasoning is skipped, no complete artifact at end.
	require.Len(t, artifacts, 1)
	assert.Equal(t, "text_delta", artifacts[0].Kind())
}

func TestProviderInvoke_ReasoningOnly(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(reasoningOnlySSE("Let me analyze", " this request")),
	}

	p, err := New(WithAPIKey("test-key"), WithModel("o3-mini"), WithHTTPClient(mockClient(transport)))
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	err = p.Invoke(t.Context(), mem, ch)
	require.NoError(t, err)

	artifacts := drainArtifacts(ch)
	// reasoning_delta x2 — only deltas emitted.
	require.Len(t, artifacts, 2)
	assert.Equal(t, "reasoning_delta", artifacts[0].Kind())
	assert.Equal(t, "Let me analyze", artifacts[0].(artifact.ReasoningDelta).Content)
	assert.Equal(t, "reasoning_delta", artifacts[1].Kind())
	assert.Equal(t, " this request", artifacts[1].(artifact.ReasoningDelta).Content)
}

func TestProviderInvoke_RoleMapping(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(simpleSSE("ok")),
	}

	p, err := New(WithAPIKey("test-key"), WithModel("gpt-4"), WithHTTPClient(mockClient(transport)))
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleSystem, artifact.Text{Content: "sys"})
	mem.Append(state.RoleUser, artifact.Text{Content: "usr"})
	mem.Append(state.RoleAssistant, artifact.Text{Content: "asst"})
	mem.Append(state.RoleTool, artifact.Text{Content: "tool"})

	ch := make(chan artifact.Artifact, 10)
	_ = p.Invoke(t.Context(), mem, ch)
	close(ch)
	for range ch {
	}

	require.NotNil(t, transport.request)
	body, _ := io.ReadAll(transport.request.Body)
	var reqBody map[string]any
	require.NoError(t, json.Unmarshal(body, &reqBody))

	msgs, ok := reqBody["messages"].([]any)
	require.True(t, ok)
	require.Len(t, msgs, 4)

	roles := []string{"system", "user", "assistant", "user"}
	for i, want := range roles {
		msg, ok := msgs[i].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, want, msg["role"])
	}
}

func TestProviderInvoke_ConcurrentOptions(t *testing.T) {
	transport := &concurrentMockTransport{
		responseBody: simpleSSE("ok"),
	}

	p, err := New(WithAPIKey("test-key"), WithModel("gpt-4"), WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			tools := []toolpkg.Tool{{Name: fmt.Sprintf("tool-%d", idx), Description: "test", Schema: map[string]any{"type": "object"}}}
			ch := make(chan artifact.Artifact, 10)
			_ = p.Invoke(t.Context(), mem, ch, WithTools(tools))
			close(ch)
			for range ch {
			}
		}(i)
	}
	wg.Wait()
}

func TestProviderInvoke_WithTemperature(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(simpleSSE("ok")),
	}

	p, err := New(WithAPIKey("test-key"), WithModel("gpt-4"), WithHTTPClient(mockClient(transport)))
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	_ = p.Invoke(t.Context(), mem, ch, WithTemperature(0.7))
	close(ch)
	for range ch {
	}

	require.NotNil(t, transport.request)
	body, _ := io.ReadAll(transport.request.Body)
	var reqBody map[string]any
	require.NoError(t, json.Unmarshal(body, &reqBody))
	assert.InDelta(t, 0.7, reqBody["temperature"], 0.001)
}

func TestProviderInvoke_WithMaxTokens(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(simpleSSE("ok")),
	}

	p, err := New(WithAPIKey("test-key"), WithModel("gpt-4"), WithHTTPClient(mockClient(transport)))
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	_ = p.Invoke(t.Context(), mem, ch, WithMaxTokens(12345))
	close(ch)
	for range ch {
	}

	require.NotNil(t, transport.request)
	body, _ := io.ReadAll(transport.request.Body)
	var reqBody map[string]any
	require.NoError(t, json.Unmarshal(body, &reqBody))
	assert.Equal(t, float64(12345), reqBody["max_tokens"])
}

func TestProviderInvoke_WithoutMaxTokens(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(simpleSSE("ok")),
	}

	p, err := New(WithAPIKey("test-key"), WithModel("gpt-4"), WithHTTPClient(mockClient(transport)))
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	_ = p.Invoke(t.Context(), mem, ch)
	close(ch)
	for range ch {
	}

	require.NotNil(t, transport.request)
	body, _ := io.ReadAll(transport.request.Body)
	var reqBody map[string]any
	require.NoError(t, json.Unmarshal(body, &reqBody))
	assert.NotContains(t, reqBody, "max_tokens")
}

func TestProviderInvoke_WithReasoningEffort(t *testing.T) {
	tests := []struct {
		name       string
		effort     string
		wantAbsent bool
	}{
		{"low", "low", false},
		{"medium", "medium", false},
		{"high", "high", false},
		{"absent when not provided", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := &mockTransport{
				response: mockResponseSSE(simpleSSE("ok")),
			}

			p, err := New(WithAPIKey("test-key"), WithModel("o3-mini"), WithHTTPClient(mockClient(transport)))
	require.NoError(t, err)
			mem := &state.Buffer{}
			mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

			ch := make(chan artifact.Artifact, 10)
			if tt.wantAbsent {
				_ = p.Invoke(t.Context(), mem, ch)
			} else {
				_ = p.Invoke(t.Context(), mem, ch, WithReasoningEffort(tt.effort))
			}
			close(ch)
			for range ch {
			}

			require.NotNil(t, transport.request)
			body, _ := io.ReadAll(transport.request.Body)
			var reqBody map[string]any
			require.NoError(t, json.Unmarshal(body, &reqBody))

			if tt.wantAbsent {
				_, ok := reqBody["reasoning_effort"]
				assert.False(t, ok, "reasoning_effort should not be present")
			} else {
				assert.Equal(t, tt.effort, reqBody["reasoning_effort"])
			}
		})
	}
}

func TestProviderInvoke_MixedAssistantTextAndToolCalls(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(simpleSSE("ok")),
	}

	p, err := New(WithAPIKey("test-key"), WithModel("gpt-4"), WithHTTPClient(mockClient(transport)))
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	// Manually append an assistant turn with both text and tool calls.
	mem.Append(state.RoleAssistant,
		artifact.Text{Content: "I'll look that up"},
		artifact.ToolCall{ID: "call_1", Name: "search", Arguments: `{"query":"test"}`},
		artifact.ToolCall{ID: "call_2", Name: "calculate", Arguments: `{"expr":"1+1"}`},
	)

	ch := make(chan artifact.Artifact, 10)
	_ = p.Invoke(t.Context(), mem, ch)
	close(ch)
	for range ch {
	}

	require.NotNil(t, transport.request)
	body, _ := io.ReadAll(transport.request.Body)
	var reqBody map[string]any
	require.NoError(t, json.Unmarshal(body, &reqBody))

	msgs, ok := reqBody["messages"].([]any)
	require.True(t, ok)
	require.Len(t, msgs, 2)

	// First message is user.
	userMsg, ok := msgs[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "user", userMsg["role"])

	// Second message is assistant with content and tool_calls.
	asstMsg, ok := msgs[1].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "assistant", asstMsg["role"])
	assert.Equal(t, "I'll look that up", asstMsg["content"])

	toolCalls, ok := asstMsg["tool_calls"].([]any)
	require.True(t, ok)
	require.Len(t, toolCalls, 2)

	tc1, ok := toolCalls[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "call_1", tc1["id"])
	fn1, ok := tc1["function"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "search", fn1["name"])

	tc2, ok := toolCalls[1].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "call_2", tc2["id"])
	fn2, ok := tc2["function"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "calculate", fn2["name"])
}

func TestProviderInvoke_ToolsWithDescription(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(simpleSSE("ok")),
	}

	tools := []toolpkg.Tool{
		{
			Name:        "add",
			Description: "Add two numbers together",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"a": map[string]any{"type": "number"},
					"b": map[string]any{"type": "number"},
				},
			},
		},
		{
			Name:        "multiply",
			Description: "Multiply two numbers together",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"a": map[string]any{"type": "number"},
					"b": map[string]any{"type": "number"},
				},
			},
		},
	}

	p, err := New(WithAPIKey("test-key"), WithModel("gpt-4"), WithHTTPClient(mockClient(transport)))
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	_ = p.Invoke(t.Context(), mem, ch, WithTools(tools))
	close(ch)
	for range ch {
	}

	require.NotNil(t, transport.request)
	body, _ := io.ReadAll(transport.request.Body)
	var reqBody map[string]any
	require.NoError(t, json.Unmarshal(body, &reqBody))

	reqTools, ok := reqBody["tools"].([]any)
	require.True(t, ok)
	require.Len(t, reqTools, 2)

	t1, ok := reqTools[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "function", t1["type"])
	fn1, ok := t1["function"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "add", fn1["name"])
	assert.Equal(t, "Add two numbers together", fn1["description"])

	t2, ok := reqTools[1].(map[string]any)
	require.True(t, ok)
	fn2, ok := t2["function"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "multiply", fn2["name"])
	assert.Equal(t, "Multiply two numbers together", fn2["description"])
}

func TestProviderInvoke_InterleavedTextReasoningChunks(t *testing.T) {
	interleavedBody := "data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"o3-mini\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hello\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"created\":2,\"model\":\"o3-mini\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"\",\"reasoning_content\":\"think\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"created\":3,\"model\":\"o3-mini\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\" world\"},\"finish_reason\":null}]}\n\n" +
		"data: [DONE]\n\n"

	transport := &mockTransport{
		response: mockResponseSSE(interleavedBody),
	}

	p, err := New(WithAPIKey("test-key"), WithModel("o3-mini"), WithHTTPClient(mockClient(transport)))
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	err = p.Invoke(t.Context(), mem, ch)
	require.NoError(t, err)

	artifacts := drainArtifacts(ch)
	// TextDelta, ReasoningDelta, TextDelta — three separate chunks in arrival order.
	require.Len(t, artifacts, 3)
	assert.Equal(t, "text_delta", artifacts[0].Kind())
	assert.Equal(t, "Hello", artifacts[0].(artifact.TextDelta).Content)
	assert.Equal(t, "reasoning_delta", artifacts[1].Kind())
	assert.Equal(t, "think", artifacts[1].(artifact.ReasoningDelta).Content)
	assert.Equal(t, "text_delta", artifacts[2].Kind())
	assert.Equal(t, " world", artifacts[2].(artifact.TextDelta).Content)
}

// mockLLMValue is a test type that implements artifact.LLMRenderer.
type mockLLMValue struct {
	output string
}

func (m *mockLLMValue) MarshalLLM() string {
	return m.output
}

func TestProviderInvoke_ToolResult_LLMRenderer(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(simpleSSE("ok")),
	}

	p, err := New(WithAPIKey("test-key"), WithModel("gpt-4"), WithHTTPClient(mockClient(transport)))
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})
	// Append an assistant turn with a tool call.
	mem.Append(state.RoleAssistant, artifact.ToolCall{ID: "call_1", Name: "search", Arguments: `{}`})
	// Append a tool result with a Value that implements LLMRenderer.
	mem.Append(state.RoleTool, artifact.ToolResult{
		ToolCallID: "call_1",
		Content:    `{"raw":"json"}`,
		Value:      &mockLLMValue{output: "custom llm output"},
	})

	ch := make(chan artifact.Artifact, 10)
	_ = p.Invoke(t.Context(), mem, ch)
	close(ch)
	for range ch {
	}

	require.NotNil(t, transport.request)
	body, _ := io.ReadAll(transport.request.Body)
	var reqBody map[string]any
	require.NoError(t, json.Unmarshal(body, &reqBody))

	msgs, ok := reqBody["messages"].([]any)
	require.True(t, ok)
	require.Len(t, msgs, 3)

	// Third message is the tool result.
	toolMsg, ok := msgs[2].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "tool", toolMsg["role"])
	assert.Equal(t, "custom llm output", toolMsg["content"])
	assert.Equal(t, "call_1", toolMsg["tool_call_id"])
}

func TestProviderInvoke_ToolResult_JSONFallback(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(simpleSSE("ok")),
	}

	p, err := New(WithAPIKey("test-key"), WithModel("gpt-4"), WithHTTPClient(mockClient(transport)))
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})
	mem.Append(state.RoleAssistant, artifact.ToolCall{ID: "call_1", Name: "search", Arguments: `{}`})
	// Tool result with a simple string Value (no LLMRenderer).
	mem.Append(state.RoleTool, artifact.ToolResult{
		ToolCallID: "call_1",
		Content:    "fallback",
		Value:      "json value",
	})

	ch := make(chan artifact.Artifact, 10)
	_ = p.Invoke(t.Context(), mem, ch)
	close(ch)
	for range ch {
	}

	require.NotNil(t, transport.request)
	body, _ := io.ReadAll(transport.request.Body)
	var reqBody map[string]any
	require.NoError(t, json.Unmarshal(body, &reqBody))

	msgs, ok := reqBody["messages"].([]any)
	require.True(t, ok)
	require.Len(t, msgs, 3)

	toolMsg, ok := msgs[2].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "tool", toolMsg["role"])
	// json.Marshal("json value") produces "\"json value\""
	assert.Equal(t, `"json value"`, toolMsg["content"])
}

func TestProviderInvoke_ToolResult_ContentFallback(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(simpleSSE("ok")),
	}

	p, err := New(WithAPIKey("test-key"), WithModel("gpt-4"), WithHTTPClient(mockClient(transport)))
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})
	mem.Append(state.RoleAssistant, artifact.ToolCall{ID: "call_1", Name: "search", Arguments: `{}`})
	// Tool result with nil Value — should fall back to Content.
	mem.Append(state.RoleTool, artifact.ToolResult{
		ToolCallID: "call_1",
		Content:    "plain content",
	})

	ch := make(chan artifact.Artifact, 10)
	_ = p.Invoke(t.Context(), mem, ch)
	close(ch)
	for range ch {
	}

	require.NotNil(t, transport.request)
	body, _ := io.ReadAll(transport.request.Body)
	var reqBody map[string]any
	require.NoError(t, json.Unmarshal(body, &reqBody))

	msgs, ok := reqBody["messages"].([]any)
	require.True(t, ok)
	require.Len(t, msgs, 3)

	toolMsg, ok := msgs[2].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "tool", toolMsg["role"])
	assert.Equal(t, "plain content", toolMsg["content"])
}

func TestProviderInvoke_ToolCallDeltaAccumulation(t *testing.T) {
	toolCallBody := "data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"search\",\"arguments\":\"first_\"}}]},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"created\":2,\"model\":\"gpt-4\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"second\"}}]},\"finish_reason\":null}]}\n\n" +
		"data: [DONE]\n\n"

	transport := &mockTransport{
		response: mockResponseSSE(toolCallBody),
	}

	p, err := New(WithAPIKey("test-key"), WithModel("gpt-4"), WithHTTPClient(mockClient(transport)))
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	err = p.Invoke(t.Context(), mem, ch)
	require.NoError(t, err)

	artifacts := drainArtifacts(ch)
	// Only raw ToolCallDelta chunks; provider no longer accumulates or emits complete ToolCall artifacts.
	require.Len(t, artifacts, 2)

	// First chunk: full ID and Name, partial Arguments.
	assert.Equal(t, "tool_call_delta", artifacts[0].Kind())
	td0, ok := artifacts[0].(artifact.ToolCallDelta)
	require.True(t, ok)
	assert.Equal(t, 0, td0.Index)
	assert.Equal(t, "call_1", td0.ID)
	assert.Equal(t, "search", td0.Name)
	assert.Equal(t, "first_", td0.Arguments)

	// Second chunk: raw chunk data (empty ID/Name in this SSE chunk), partial Arguments.
	assert.Equal(t, "tool_call_delta", artifacts[1].Kind())
	td1, ok := artifacts[1].(artifact.ToolCallDelta)
	require.True(t, ok)
	assert.Equal(t, 0, td1.Index)
	assert.Empty(t, td1.ID)
	assert.Empty(t, td1.Name)
	assert.Equal(t, "second", td1.Arguments)
}

func TestProviderInvoke_EmptyToolsOmitted(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(simpleSSE("ok")),
	}

	p, err := New(WithAPIKey("test-key"), WithModel("gpt-4"), WithHTTPClient(mockClient(transport)))
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	_ = p.Invoke(t.Context(), mem, ch, WithTools([]toolpkg.Tool{}))
	close(ch)
	for range ch {
	}

	require.NotNil(t, transport.request)
	body, _ := io.ReadAll(transport.request.Body)
	var reqBody map[string]any
	require.NoError(t, json.Unmarshal(body, &reqBody))

	_, ok := reqBody["tools"]
	assert.False(t, ok, "tools field should not be present when empty slice is passed")
}

func TestProviderInvoke_DynamicToolsOption(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(simpleSSE("ok")),
	}

	p, err := New(WithAPIKey("test-key"), WithModel("gpt-4"), WithHTTPClient(mockClient(transport)))
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	// Dynamic ToolsOption whose function inspects context and state.
	dynamicOpt := provider.ToolsOption{
		Tools: func(ctx context.Context, st state.State) []toolpkg.Tool {
			assert.NotNil(t, ctx)
			turns := st.Turns()
			assert.Len(t, turns, 1)
			assert.Equal(t, state.RoleUser, turns[0].Role)

			return []toolpkg.Tool{
				{
					Name:        "dynamic_tool",
					Description: "a dynamic tool",
					Schema:      map[string]any{"type": "object"},
				},
			}
		},
	}

	ch := make(chan artifact.Artifact, 10)
	_ = p.Invoke(t.Context(), mem, ch, dynamicOpt)
	close(ch)
	for range ch {
	}

	require.NotNil(t, transport.request)
	body, _ := io.ReadAll(transport.request.Body)
	var reqBody map[string]any
	require.NoError(t, json.Unmarshal(body, &reqBody))

	reqTools, ok := reqBody["tools"].([]any)
	require.True(t, ok)
	require.Len(t, reqTools, 1)

	t1, ok := reqTools[0].(map[string]any)
	require.True(t, ok)
	fn1, ok := t1["function"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "dynamic_tool", fn1["name"])
	assert.Equal(t, "a dynamic tool", fn1["description"])
}

func TestProviderInvoke_ToolsOptionPrecedence(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(simpleSSE("ok")),
	}

	p, err := New(WithAPIKey("test-key"), WithModel("gpt-4"), WithHTTPClient(mockClient(transport)))
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	// Later ToolsOption should override the earlier one.
	_ = p.Invoke(t.Context(), mem, ch,
		provider.WithTools([]toolpkg.Tool{{Name: "first", Description: "first tool"}}),
		provider.ToolsOption{Tools: func(context.Context, state.State) []toolpkg.Tool {
			return []toolpkg.Tool{
				{
					Name:        "second",
					Description: "second tool",
					Schema:      map[string]any{"type": "object"},
				},
			}
		}},
	)
	close(ch)
	for range ch {
	}

	require.NotNil(t, transport.request)
	body, _ := io.ReadAll(transport.request.Body)
	var reqBody map[string]any
	require.NoError(t, json.Unmarshal(body, &reqBody))

	reqTools, ok := reqBody["tools"].([]any)
	require.True(t, ok)
	require.Len(t, reqTools, 1)

	t1, ok := reqTools[0].(map[string]any)
	require.True(t, ok)
	fn1, ok := t1["function"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "second", fn1["name"], "later ToolsOption should take precedence")
}

func TestProviderInvoke_DynamicTools_EmptyResult(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(simpleSSE("ok")),
	}

	p, err := New(WithAPIKey("test-key"), WithModel("gpt-4"), WithHTTPClient(mockClient(transport)))
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	_ = p.Invoke(t.Context(), mem, ch, provider.ToolsOption{
		Tools: func(context.Context, state.State) []toolpkg.Tool {
			return nil
		},
	})
	close(ch)
	for range ch {
	}

	require.NotNil(t, transport.request)
	body, _ := io.ReadAll(transport.request.Body)
	var reqBody map[string]any
	require.NoError(t, json.Unmarshal(body, &reqBody))

	_, ok := reqBody["tools"]
	assert.False(t, ok, "tools field should not be present when dynamic filter returns nil")
}

func TestProviderInvoke_DynamicTools_Concurrency(t *testing.T) {
	transport := &recordingMockTransport{
		responseBody: simpleSSE("ok"),
	}

	p, err := New(WithAPIKey("test-key"), WithModel("gpt-4"), WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			tools := []toolpkg.Tool{
				{
					Name:        fmt.Sprintf("tool-%d", idx),
					Description: "test",
					Schema:      map[string]any{"type": "object"},
				},
			}
			ch := make(chan artifact.Artifact, 10)
			_ = p.Invoke(t.Context(), mem, ch, provider.ToolsOption{
				Tools: func(context.Context, state.State) []toolpkg.Tool {
					return tools
				},
			})
			close(ch)
			for range ch {
			}
		}(i)
	}
	wg.Wait()

	requests := transport.Requests()
	require.Len(t, requests, 10)

	for i, rr := range requests {
		var reqBody map[string]any
		require.NoError(t, json.Unmarshal(rr.body, &reqBody))

		reqTools, ok := reqBody["tools"].([]any)
		require.True(t, ok, "request %d should contain tools", i)
		require.Len(t, reqTools, 1, "request %d should contain exactly one tool", i)

		t1, ok := reqTools[0].(map[string]any)
		require.True(t, ok)
		fn1, ok := t1["function"].(map[string]any)
		require.True(t, ok)

		found := false
		for j := 0; j < 10; j++ {
			if fn1["name"] == fmt.Sprintf("tool-%d", j) {
				found = true
				break
			}
		}
		assert.True(t, found, "request %d should have a valid tool name, got %v", i, fn1["name"])
	}
}

func TestProviderInvoke_WithFilteredTools(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(simpleSSE("ok")),
	}

	p, err := New(WithAPIKey("test-key"), WithModel("gpt-4"), WithHTTPClient(mockClient(transport)))
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	registry := toolpkg.NewRegistry()
	require.NoError(t, registry.Register(toolpkg.Tool{Name: "allowed", Description: "Allowed tool", Schema: map[string]any{"type": "object"}}, func(context.Context, toolpkg.Sandbox, map[string]any) (any, error) {
		return nil, nil
	}))
	require.NoError(t, registry.Register(toolpkg.Tool{Name: "denied", Description: "Denied tool", Schema: map[string]any{"type": "object"}}, func(context.Context, toolpkg.Sandbox, map[string]any) (any, error) {
		return nil, nil
	}))

	filter := func(ctx context.Context, st state.State, tools []toolpkg.Tool) []toolpkg.Tool {
		var result []toolpkg.Tool
		for _, tool := range tools {
			if tool.Name == "allowed" {
				result = append(result, tool)
			}
		}
		return result
	}

	ch := make(chan artifact.Artifact, 10)
	_ = p.Invoke(t.Context(), mem, ch, xtool.WithFilteredTools(registry, filter))
	close(ch)
	for range ch {
	}

	require.NotNil(t, transport.request)
	body, _ := io.ReadAll(transport.request.Body)
	var reqBody map[string]any
	require.NoError(t, json.Unmarshal(body, &reqBody))

	reqTools, ok := reqBody["tools"].([]any)
	require.True(t, ok)
	require.Len(t, reqTools, 1)

	t1, ok := reqTools[0].(map[string]any)
	require.True(t, ok)
	fn1, ok := t1["function"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "allowed", fn1["name"])
	assert.Equal(t, "Allowed tool", fn1["description"])
}

func TestProviderInvoke_WithUsage(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(textWithUsageSSE("Hello, world!", 10, 5, 15)),
	}

	p, err := New(WithAPIKey("test-key"), WithModel("gpt-4"), WithHTTPClient(mockClient(transport)))
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	err = p.Invoke(t.Context(), mem, ch)
	require.NoError(t, err)

	artifacts := drainArtifacts(ch)
	require.Len(t, artifacts, 2)
	assert.Equal(t, "text_delta", artifacts[0].Kind())
	assert.Equal(t, "Hello, world!", artifacts[0].(artifact.TextDelta).Content)

	usage, ok := artifacts[1].(artifact.Usage)
	require.True(t, ok, "second artifact should be Usage")
	assert.Equal(t, 10, usage.PromptTokens)
	assert.Equal(t, 5, usage.CompletionTokens)
	assert.Equal(t, 15, usage.TotalTokens)
}

func TestProviderInvoke_WithoutUsage(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(simpleSSE("Hello, world!")),
	}

	p, err := New(WithAPIKey("test-key"), WithModel("gpt-4"), WithHTTPClient(mockClient(transport)))
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	err = p.Invoke(t.Context(), mem, ch)
	require.NoError(t, err)

	artifacts := drainArtifacts(ch)
	// Only text_delta; no usage artifact because the SSE does not contain a usage chunk.
	require.Len(t, artifacts, 1)
	assert.Equal(t, "text_delta", artifacts[0].Kind())
}

func TestProviderInvoke_IncludeUsageFlag(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(simpleSSE("ok")),
	}

	p, err := New(WithAPIKey("test-key"), WithModel("gpt-4"), WithHTTPClient(mockClient(transport)))
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	_ = p.Invoke(t.Context(), mem, ch)
	close(ch)
	for range ch {
	}

	require.NotNil(t, transport.request)
	body, _ := io.ReadAll(transport.request.Body)
	var reqBody map[string]any
	require.NoError(t, json.Unmarshal(body, &reqBody))

	streamOptions, ok := reqBody["stream_options"].(map[string]any)
	require.True(t, ok, "stream_options should be present in request body")
	assert.Equal(t, true, streamOptions["include_usage"])
}

func TestProviderInvoke_PartialStreamError(t *testing.T) {
	wantErr := errors.New("connection reset")

	// One valid SSE chunk followed by a read error.
	body := "data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"},\"finish_reason\":null}]}\n\n"
	transport := &mockTransport{
		response: &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(&errorReader{data: []byte(body), err: wantErr}),
		},
	}

	p, err := New(WithAPIKey("test-key"), WithModel("gpt-4"), WithHTTPClient(mockClient(transport)))
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	err = p.Invoke(t.Context(), mem, ch)

	// The channel should contain the partial artifact emitted before the error.
	close(ch)
	var artifacts []artifact.Artifact
	for art := range ch {
		artifacts = append(artifacts, art)
	}

	require.Len(t, artifacts, 1)
	assert.Equal(t, "text_delta", artifacts[0].Kind())
	assert.Equal(t, "partial", artifacts[0].(artifact.TextDelta).Content)

	require.Error(t, err)
	assert.Contains(t, err.Error(), wantErr.Error())
}

func TestProviderInvoke_HTTPTraceContext(t *testing.T) {
	tests := []struct {
		name       string
		withTracer bool
		wantTrace  bool
	}{
		{"with tracer injects httptrace", true, true},
		{"without tracer no httptrace", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := &mockTransport{
				response: mockResponseSSE(simpleSSE("ok")),
			}

			var opts []Option
			opts = append(opts, WithAPIKey("test-key"), WithModel("gpt-4"), WithHTTPClient(mockClient(transport)))
			if tt.withTracer {
				opts = append(opts, WithTracer(trace.NewNoopTracerProvider().Tracer("test")))
			}

			p, err := New(opts...)
			require.NoError(t, err)

			mem := &state.Buffer{}
			mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

			ch := make(chan artifact.Artifact, 10)
			_ = p.Invoke(t.Context(), mem, ch)
			close(ch)
			for range ch {
			}

			require.NotNil(t, transport.request)
			ct := httptrace.ContextClientTrace(transport.request.Context())
			if tt.wantTrace {
				assert.NotNil(t, ct, "httptrace.ClientTrace should be present in request context")
			} else {
				assert.Nil(t, ct, "httptrace.ClientTrace should not be present when no tracer is configured")
			}
		})
	}
}
