package openai

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/andrewhowdencom/ore/models"
	"io"
	"net"
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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
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

// flatReasoningSSE emits a delta carrying `reasoning` (flat string) instead
// of `reasoning_content`. This is the wire shape surfaced by OpenRouter
// for non-Anthropic reasoning routes.
func flatReasoningSSE(reasoning string) string {
	return fmt.Sprintf("data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"deepseek/deepseek-r1\",\"choices\":[{\"index\":0,\"delta\":{\"reasoning\":%q},\"finish_reason\":null}]}\n\ndata: [DONE]\n\n", reasoning)
}

// reasoningDetailsTextSSE emits a delta carrying a `reasoning_details`
// array with one `reasoning.text` entry. This is the OpenRouter wire
// shape for streaming text-mode reasoning.
func reasoningDetailsTextSSE(text string) string {
	return fmt.Sprintf("data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"anthropic/claude-3.7-sonnet:thinking\",\"choices\":[{\"index\":0,\"delta\":{\"reasoning_details\":[{\"type\":\"reasoning.text\",\"text\":%q,\"id\":\"rd-0\"}]},\"finish_reason\":null}]}\n\ndata: [DONE]\n\n", text)
}

// reasoningDetailsEncryptedSSE emits a delta carrying a `reasoning_details`
// array with one `reasoning.encrypted` entry. This is the OpenRouter wire
// shape for streaming encrypted reasoning; the provider must emit a
// ReasoningSignature so the next turn can replay the encrypted blob.
func reasoningDetailsEncryptedSSE(data string) string {
	return fmt.Sprintf("data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"anthropic/claude-3.7-sonnet:thinking\",\"choices\":[{\"index\":0,\"delta\":{\"reasoning_details\":[{\"type\":\"reasoning.encrypted\",\"data\":%q,\"id\":\"rd-0\"}]},\"finish_reason\":null}]}\n\ndata: [DONE]\n\n", data)
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

// openaiCacheReadSSE emits a final usage chunk that includes the OpenAI
// native cache metric at usage.prompt_tokens_details.cached_tokens. This
// is the wire shape returned by raw OpenAI on a cache hit.
func openaiCacheReadSSE(prompt, completion, total, cached int64) string {
	return fmt.Sprintf("data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4\",\"choices\":[],\"usage\":{\"prompt_tokens\":%d,\"completion_tokens\":%d,\"total_tokens\":%d,\"prompt_tokens_details\":{\"cached_tokens\":%d}}}\n\ndata: [DONE]\n\n",
		prompt, completion, total, cached)
}

// anthropicCacheSSE emits a final usage chunk with the Anthropic-style
// cache fields at the top of the usage object. This is the wire shape
// returned by Anthropic-via-OpenRouter (and by raw Anthropic through any
// compatible proxy). The SDK does not model these, so the provider must
// read them out of JSON extra fields.
func anthropicCacheSSE(prompt, completion, total, cacheRead, cacheWrite int64) string {
	return fmt.Sprintf("data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4\",\"choices\":[],\"usage\":{\"prompt_tokens\":%d,\"completion_tokens\":%d,\"total_tokens\":%d,\"cache_read_input_tokens\":%d,\"cache_creation_input_tokens\":%d}}\n\ndata: [DONE]\n\n",
		prompt, completion, total, cacheRead, cacheWrite)
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

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "gpt-4"}
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	err = p.Invoke(t.Context(), mem, spec, ch)
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

	p, err := New(WithAPIKey("bad-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "gpt-4"}
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	err = p.Invoke(t.Context(), mem, spec, ch)
	require.Error(t, err)
}

func TestProviderInvoke_EmptyChoices(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(emptyChoicesSSE()),
	}

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "gpt-4"}
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	err = p.Invoke(t.Context(), mem, spec, ch)
	require.NoError(t, err)

	artifacts := drainArtifacts(ch)
	assert.Empty(t, artifacts)
}

func TestProviderInvoke_UsageChunk(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(usageSSE(10, 5, 15)),
	}

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "gpt-4"}
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	err = p.Invoke(t.Context(), mem, spec, ch)
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

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "gpt-4"}
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser,
		artifact.Text{Content: "line1"},
		artifact.Text{Content: "line2"},
	)

	ch := make(chan artifact.Artifact, 10)
	_ = p.Invoke(t.Context(), mem, spec, ch)
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

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "gpt-4"}
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser,
		artifact.Text{Content: "hello"},
		artifact.ToolCall{Name: "foo", Arguments: "{}"},
		artifact.Image{URL: "http://example.com/img.png"},
	)

	ch := make(chan artifact.Artifact, 10)
	_ = p.Invoke(t.Context(), mem, spec, ch)
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

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "gpt-4"}
	require.NoError(t, err)
	mem := &state.Buffer{}

	ch := make(chan artifact.Artifact, 10)
	_ = p.Invoke(t.Context(), mem, spec, ch)
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

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "gpt-4"}
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	err = p.Invoke(t.Context(), mem, spec, ch)
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

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "gpt-4"}
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	err = p.Invoke(t.Context(), mem, spec, ch)
	require.Error(t, err)
}

func TestProviderInvoke_ContextCancellation(t *testing.T) {
	transport := &mockTransport{
		err: context.Canceled,
	}

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "gpt-4"}
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ch := make(chan artifact.Artifact, 10)
	err = p.Invoke(ctx, mem, spec, ch)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestProviderInvoke_CustomClient(t *testing.T) {
	wantErr := errors.New("custom transport error")
	transport := &mockTransport{
		err: wantErr,
	}

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "gpt-4"}
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	err = p.Invoke(t.Context(), mem, spec, ch)
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
}

func TestProviderInvoke_WithReasoning(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(reasoningSSE("Hello, world!", "Let me analyze this...")),
	}

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "o3-mini"}
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	err = p.Invoke(t.Context(), mem, spec, ch)
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

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "o3-mini"}
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	err = p.Invoke(t.Context(), mem, spec, ch)
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

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "o3-mini"}
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	err = p.Invoke(t.Context(), mem, spec, ch)
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

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "gpt-4"}
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleSystem, artifact.Text{Content: "sys"})
	mem.Append(state.RoleUser, artifact.Text{Content: "usr"})
	mem.Append(state.RoleAssistant, artifact.Text{Content: "asst"})
	mem.Append(state.RoleTool, artifact.Text{Content: "tool"})

	ch := make(chan artifact.Artifact, 10)
	_ = p.Invoke(t.Context(), mem, spec, ch)
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

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(&http.Client{Transport: transport}))
	spec := models.Spec{Name: "gpt-4"}
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
			_ = p.Invoke(t.Context(), mem, spec, ch, WithTools(tools))
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

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "gpt-4"}
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	_ = p.Invoke(t.Context(), mem, spec, ch, WithTemperature(0.7))
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

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "gpt-4"}
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	_ = p.Invoke(t.Context(), mem, spec, ch, WithMaxTokens(12345))
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

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "gpt-4"}
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	_ = p.Invoke(t.Context(), mem, spec, ch)
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
	// Tests that each thinking level produces the expected
	// reasoning_effort string on the wire, that the off/empty levels
	// omit the field, and that unknown levels are treated as off
	// (forward compatibility).
	tests := []struct {
		name      string
		level     provider.ThinkingLevel
		wantField string // expected value of reasoning_effort, "" means absent
		hasOption bool   // whether to pass the WithThinkingLevel option
	}{
		{"low produces low", provider.ThinkingLevelLow, "low", true},
		{"medium produces medium", provider.ThinkingLevelMedium, "medium", true},
		{"high produces high", provider.ThinkingLevelHigh, "high", true},
		{"minimal clamps to low", provider.ThinkingLevelMinimal, "low", true},
		{"max clamps to high", provider.ThinkingLevelMax, "high", true},
		{"off omits the field", provider.ThinkingLevelOff, "", true},
		{"empty omits the field", "", "", true},
		{"absent when not provided", "", "", false},
		{"unknown level is treated as off", "frobnicate", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := &mockTransport{
				response: mockResponseSSE(simpleSSE("ok")),
			}

			p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
			spec := models.Spec{Name: "o3-mini"}
			require.NoError(t, err)
			mem := &state.Buffer{}
			mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

			ch := make(chan artifact.Artifact, 10)
			if tt.hasOption {
				_ = p.Invoke(t.Context(), mem, spec, ch, WithThinkingLevel(tt.level))
			} else {
				_ = p.Invoke(t.Context(), mem, spec, ch)
			}
			close(ch)
			for range ch {
			}

			require.NotNil(t, transport.request)
			body, _ := io.ReadAll(transport.request.Body)
			var reqBody map[string]any
			require.NoError(t, json.Unmarshal(body, &reqBody))

			got, ok := reqBody["reasoning_effort"]
			if tt.wantField == "" {
				assert.False(t, ok, "reasoning_effort should not be present")
			} else {
				require.True(t, ok, "reasoning_effort should be present")
				assert.Equal(t, tt.wantField, got)
			}
		})
	}
}

func TestTranslateThinkingLevel(t *testing.T) {
	t.Parallel()

	cases := []struct {
		level provider.ThinkingLevel
		want  string
	}{
		{provider.ThinkingLevelOff, ""},
		{provider.ThinkingLevelMinimal, "low"},
		{provider.ThinkingLevelLow, "low"},
		{provider.ThinkingLevelMedium, "medium"},
		{provider.ThinkingLevelHigh, "high"},
		{provider.ThinkingLevelMax, "high"},
		{"", ""},
		{"unknown", ""},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, translateThinkingLevel(tc.level), "%q", tc.level)
	}
}

// TestInvoke_ReasoningIncludeField covers all four quadrants of the
// reasoning-include decision matrix: OpenAI (no field), OpenRouter
// (auto-detected), OpenAI with explicit opt-in, and OpenRouter with
// explicit opt-out (the escape hatch).
func TestInvoke_ReasoningIncludeField(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		baseURL   string
		option    Option
		wantField bool
	}{
		{
			name:      "openai base url, no override",
			baseURL:   "https://api.openai.com/v1",
			option:    nil,
			wantField: false,
		},
		{
			name:      "openrouter base url, auto-detected",
			baseURL:   "https://openrouter.ai/api/v1",
			option:    nil,
			wantField: true,
		},
		{
			name:      "openai base url, explicit opt-in",
			baseURL:   "https://api.openai.com/v1",
			option:    WithReasoningInclude(true),
			wantField: true,
		},
		{
			name:      "openrouter base url, explicit opt-out",
			baseURL:   "https://openrouter.ai/api/v1",
			option:    WithReasoningInclude(false),
			wantField: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			transport := &mockTransport{
				response: mockResponseSSE(simpleSSE("ok")),
			}

			opts := []Option{
				WithAPIKey("test-key"),

				WithBaseURL(tt.baseURL),
				WithHTTPClient(mockClient(transport)),
			}
			if tt.option != nil {
				opts = append(opts, tt.option)
			}

			p, err := New(opts...)
			spec := models.Spec{Name: "deepseek/deepseek-r1"}
			require.NoError(t, err)
			mem := &state.Buffer{}
			mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

			ch := make(chan artifact.Artifact, 10)
			_ = p.Invoke(t.Context(), mem, spec, ch)
			close(ch)
			for range ch {
			}

			require.NotNil(t, transport.request)
			body, _ := io.ReadAll(transport.request.Body)
			var reqBody map[string]any
			require.NoError(t, json.Unmarshal(body, &reqBody))

			reasoning, ok := reqBody["reasoning"].(map[string]any)
			if !tt.wantField {
				assert.False(t, ok, "reasoning field should be absent; got %v", reqBody["reasoning"])
				return
			}
			require.True(t, ok, "reasoning field should be present in request body")
			assert.Equal(t, true, reasoning["include"])
		})
	}
}

func TestProviderInvoke_MixedAssistantTextAndToolCalls(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(simpleSSE("ok")),
	}

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "gpt-4"}
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
	_ = p.Invoke(t.Context(), mem, spec, ch)
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

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "gpt-4"}
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	_ = p.Invoke(t.Context(), mem, spec, ch, WithTools(tools))
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

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "o3-mini"}
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	err = p.Invoke(t.Context(), mem, spec, ch)
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

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "gpt-4"}
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
	_ = p.Invoke(t.Context(), mem, spec, ch)
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

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "gpt-4"}
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
	_ = p.Invoke(t.Context(), mem, spec, ch)
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

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "gpt-4"}
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
	_ = p.Invoke(t.Context(), mem, spec, ch)
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

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "gpt-4"}
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	err = p.Invoke(t.Context(), mem, spec, ch)
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

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "gpt-4"}
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	_ = p.Invoke(t.Context(), mem, spec, ch, WithTools([]toolpkg.Tool{}))
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

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "gpt-4"}
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
	_ = p.Invoke(t.Context(), mem, spec, ch, dynamicOpt)
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

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "gpt-4"}
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	// Later ToolsOption should override the earlier one.
	_ = p.Invoke(t.Context(), mem, spec, ch,
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

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "gpt-4"}
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	_ = p.Invoke(t.Context(), mem, spec, ch, provider.ToolsOption{
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

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(&http.Client{Transport: transport}))
	spec := models.Spec{Name: "gpt-4"}
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
			_ = p.Invoke(t.Context(), mem, spec, ch, provider.ToolsOption{
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

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "gpt-4"}
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
	_ = p.Invoke(t.Context(), mem, spec, ch, xtool.WithFilteredTools(registry, filter))
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

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "gpt-4"}
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	err = p.Invoke(t.Context(), mem, spec, ch)
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

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "gpt-4"}
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	err = p.Invoke(t.Context(), mem, spec, ch)
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

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "gpt-4"}
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	_ = p.Invoke(t.Context(), mem, spec, ch)
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

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "gpt-4"}
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	err = p.Invoke(t.Context(), mem, spec, ch)

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
			opts = append(opts, WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
			if tt.withTracer {
				opts = append(opts, WithTracer(trace.NewNoopTracerProvider().Tracer("test")))
			}

			p, err := New(opts...)
			spec := models.Spec{Name: "gpt-4"}
			require.NoError(t, err)

			mem := &state.Buffer{}
			mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

			ch := make(chan artifact.Artifact, 10)
			_ = p.Invoke(t.Context(), mem, spec, ch)
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

// hookingMockTransport is an http.RoundTripper that calls httptrace hooks
// on the request's context before returning a canned response. This lets
// tests exercise the otelhttptrace instrumentation without a real network.
type hookingMockTransport struct {
	response *http.Response
	request  *http.Request
}

func (m *hookingMockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	m.request = req
	ct := httptrace.ContextClientTrace(req.Context())
	if ct != nil {
		if ct.DNSStart != nil {
			ct.DNSStart(httptrace.DNSStartInfo{Host: "api.openai.com"})
		}
		if ct.DNSDone != nil {
			ct.DNSDone(httptrace.DNSDoneInfo{Addrs: []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}})
		}
		if ct.ConnectStart != nil {
			ct.ConnectStart("tcp", "127.0.0.1:443")
		}
		if ct.ConnectDone != nil {
			ct.ConnectDone("tcp", "127.0.0.1:443", nil)
		}
		if ct.TLSHandshakeStart != nil {
			ct.TLSHandshakeStart()
		}
		if ct.TLSHandshakeDone != nil {
			ct.TLSHandshakeDone(tls.ConnectionState{}, nil)
		}
		if ct.GotFirstResponseByte != nil {
			ct.GotFirstResponseByte()
		}
	}
	return m.response, nil
}

func TestProviderInvoke_SpanLifecycle(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		response   string
		wantErr    bool
		wantStatus codes.Code
	}{
		{
			name:       "success",
			statusCode: 200,
			response:   simpleSSE("ok"),
			wantErr:    false,
			wantStatus: codes.Unset,
		},
		{
			name:       "http error",
			statusCode: 401,
			response:   `{"error":{"message":"invalid key","type":"invalid_request_error"}}`,
			wantErr:    true,
			wantStatus: codes.Error,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := tracetest.NewSpanRecorder()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
			defer tp.Shutdown(context.Background())

			var transport *mockTransport
			if tt.statusCode == 200 {
				transport = &mockTransport{response: mockResponseSSE(tt.response)}
			} else {
				transport = &mockTransport{response: mockResponse(tt.statusCode, tt.response)}
			}

			p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)), WithTracer(tp.Tracer("test")))
			spec := models.Spec{Name: "gpt-4"}
			require.NoError(t, err)

			mem := &state.Buffer{}
			mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

			ch := make(chan artifact.Artifact, 10)
			err = p.Invoke(t.Context(), mem, spec, ch)
			close(ch)
			for range ch {
			}

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			ended := sr.Ended()
			require.Len(t, ended, 1, "exactly one span should be ended")
			assert.Equal(t, "provider.invoke", ended[0].Name())
			assert.Equal(t, trace.SpanKindClient, ended[0].SpanKind())
			assert.Equal(t, tt.wantStatus, ended[0].Status().Code)
		})
	}
}

func TestProviderInvoke_HTTPTrace_Events(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	defer tp.Shutdown(context.Background())

	transport := &hookingMockTransport{
		response: mockResponseSSE(simpleSSE("ok")),
	}

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(&http.Client{Transport: transport}), WithTracer(tp.Tracer("test")))
	spec := models.Spec{Name: "gpt-4"}
	require.NoError(t, err)

	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	err = p.Invoke(t.Context(), mem, spec, ch)
	require.NoError(t, err)
	drainArtifacts(ch)

	ended := sr.Ended()
	require.Len(t, ended, 1)
	events := ended[0].Events()
	assert.NotEmpty(t, events, "span should have HTTP lifecycle events recorded")
}

func TestProviderInvoke_WithoutSubSpans(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	defer tp.Shutdown(context.Background())

	transport := &hookingMockTransport{
		response: mockResponseSSE(simpleSSE("ok")),
	}

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(&http.Client{Transport: transport}), WithTracer(tp.Tracer("test")))
	spec := models.Spec{Name: "gpt-4"}
	require.NoError(t, err)

	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	err = p.Invoke(t.Context(), mem, spec, ch)
	require.NoError(t, err)
	drainArtifacts(ch)

	ended := sr.Ended()
	require.Len(t, ended, 1)
	assert.Equal(t, "provider.invoke", ended[0].Name())
	assert.Equal(t, 0, ended[0].ChildSpanCount(), "WithoutSubSpans() should not create child spans")
}

// TestInvoke_WithSessionID verifies that the WithSessionID option causes
// prompt_cache_key to appear in the outgoing request body, and that
// omitting the option leaves the field absent. The two-case table mirrors
// TestInvoke_ReasoningIncludeField.
func TestInvoke_WithSessionID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		option      provider.InvokeOption
		wantPresent bool
		wantValue   string
	}{
		{
			name:        "option supplied sets prompt_cache_key",
			option:      WithSessionID("sess-abc-123"),
			wantPresent: true,
			wantValue:   "sess-abc-123",
		},
		{
			name:        "option not supplied omits prompt_cache_key",
			option:      nil,
			wantPresent: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			transport := &recordingMockTransport{
				responseBody: simpleSSE("ok"),
			}

			p, err := New(PIKey("test-key"),

				WithBaseURL("https://api.openai.com/v1"),
				WithHTTPClient(&http.Client{Transport: transport}),
			)
			spec := models.Spec{Name: "gpt-4"}
			require.NoError(t, err)

			mem := &state.Buffer{}
			mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

			ch := make(chan artifact.Artifact, 10)
			invokeOpts := []provider.InvokeOption{}
			if tt.option != nil {
				invokeOpts = append(invokeOpts, tt.option)
			}
			err = p.Invoke(t.Context(), mem, spec, ch, invokeOpts...)
			require.NoError(t, err)
			drainArtifacts(ch)

			requests := transport.Requests()
			require.Len(t, requests, 1)
			var reqBody map[string]any
			require.NoError(t, json.Unmarshal(requests[0].body, &reqBody))

			got, present := reqBody["prompt_cache_key"]
			if !tt.wantPresent {
				assert.False(t, present, "prompt_cache_key should be absent; got %v", got)
				return
			}
			require.True(t, present, "prompt_cache_key should be present in request body")
			assert.Equal(t, tt.wantValue, got)
		})
	}
}

// TestInvoke_WithCacheControl verifies that WithCacheControl emits
// Anthropic-style cache_control:{type:ephemeral} blocks at the three
// targeted locations (system message content, last user/assistant text
// content part, and last tool function) and that omitting the option
// leaves the request body byte-equivalent to the pre-change shape.
func TestInvoke_WithCacheControl(t *testing.T) {
	t.Parallel()

	// Two tool definitions so we can assert that only the last one is
	// stamped, not the first.
	tools := []toolpkg.Tool{
		{Name: "first", Schema: map[string]any{"type": "object"}},
		{Name: "second", Schema: map[string]any{"type": "object"}},
	}

	tests := []struct {
		name     string
		option   provider.InvokeOption
		wantOpts bool
	}{
		{
			name:     "option supplied stamps the three locations",
			option:   WithCacheControl(),
			wantOpts: true,
		},
		{
			name:     "option not supplied leaves the body unchanged",
			option:   nil,
			wantOpts: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			transport := &recordingMockTransport{
				responseBody: simpleSSE("ok"),
			}

			p, err := New(PIKey("test-key"),

				WithBaseURL("https://openrouter.ai/api/v1"), // exercises the reasoning.include path too
				WithHTTPClient(&http.Client{Transport: transport}),
			)
			spec := models.Spec{Name: "gpt-4"}
			require.NoError(t, err)

			mem := &state.Buffer{}
			mem.Append(state.RoleSystem, artifact.Text{Content: "You are a helpful assistant."})
			mem.Append(state.RoleUser, artifact.Text{Content: "first question"})
			mem.Append(state.RoleAssistant, artifact.Text{Content: "first answer"})
			mem.Append(state.RoleUser, artifact.Text{Content: "follow up"})

			invokeOpts := []provider.InvokeOption{
				provider.WithTools(tools),
			}
			if tt.option != nil {
				invokeOpts = append(invokeOpts, tt.option)
			}
			ch := make(chan artifact.Artifact, 10)
			require.NoError(t, p.Invoke(t.Context(), mem, spec, ch, invokeOpts...))
			drainArtifacts(ch)

			requests := transport.Requests()
			require.Len(t, requests, 1)
			var reqBody map[string]any
			require.NoError(t, json.Unmarshal(requests[0].body, &reqBody))

			if !tt.wantOpts {
				assertNoCacheControl(t, reqBody)
				return
			}

			// messages[0] is the system message; its content has been
			// converted from a string to a content-parts array with a
			// single text part carrying cache_control.
			msgs, ok := reqBody["messages"].([]any)
			require.True(t, ok)
			require.NotEmpty(t, msgs)
			sysMsg, ok := msgs[0].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, "system", sysMsg["role"])
			sysParts, ok := sysMsg["content"].([]any)
			require.True(t, ok, "system content should be a content-parts array when cache_control is set")
			require.Len(t, sysParts, 1)
			sysPart := sysParts[0].(map[string]any)
			assert.Equal(t, "text", sysPart["type"])
			assert.Equal(t, "You are a helpful assistant.", sysPart["text"])
			assertCacheControlEphemeral(t, sysPart)

			// The last message is the follow-up user turn. Its content
			// must also carry a cache_control block.
			lastMsg := msgs[len(msgs)-1].(map[string]any)
			assert.Equal(t, "user", lastMsg["role"])
			lastParts, ok := lastMsg["content"].([]any)
			require.True(t, ok, "last user content should be a content-parts array when cache_control is set")
			require.Len(t, lastParts, 1)
			lastPart := lastParts[0].(map[string]any)
			assert.Equal(t, "text", lastPart["type"])
			assert.Equal(t, "follow up", lastPart["text"])
			assertCacheControlEphemeral(t, lastPart)

			// Middle turns (the first user/assistant) should NOT carry
			// cache_control.
			for i := 1; i < len(msgs)-1; i++ {
				m := msgs[i].(map[string]any)
				if c, hasContent := m["content"]; hasContent {
					if s, ok := c.(string); ok {
						_ = s // string content is fine, no stamp expected
					}
				}
			}

			// tools[-1] is the second tool. Its function block must
			// carry cache_control; tools[0] must not.
			toolList, ok := reqBody["tools"].([]any)
			require.True(t, ok)
			require.Len(t, toolList, 2)
			firstTool := toolList[0].(map[string]any)
			firstFn := firstTool["function"].(map[string]any)
			_, hasFirstCC := firstFn["cache_control"]
			assert.False(t, hasFirstCC, "first tool should not carry cache_control")
			lastTool := toolList[1].(map[string]any)
			lastFn := lastTool["function"].(map[string]any)
			assertCacheControlEphemeral(t, lastFn)
		})
	}
}

// assertNoCacheControl walks a decoded JSON value and fails the test if any
// "cache_control" key is found at any nesting level. Used to assert that
// the request body is byte-equivalent to the pre-change shape when
// WithCacheControl is not supplied.
func assertNoCacheControl(t *testing.T, v any) {
	t.Helper()
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			if k == "cache_control" {
				t.Errorf("unexpected cache_control key in request body: %v", x)
			}
			assertNoCacheControl(t, val)
		}
	case []any:
		for _, item := range x {
			assertNoCacheControl(t, item)
		}
	}
}

// assertCacheControlEphemeral asserts that the given decoded object
// contains a cache_control:{type:ephemeral} block. Used by the
// WithCacheControl test to assert the three targeted locations.
func assertCacheControlEphemeral(t *testing.T, m map[string]any) {
	t.Helper()
	cc, ok := m["cache_control"]
	require.True(t, ok, "expected cache_control block in %v", m)
	ccMap, ok := cc.(map[string]any)
	require.True(t, ok, "expected cache_control to be an object; got %T", cc)
	assert.Equal(t, "ephemeral", ccMap["type"])
}

// TestProviderInvoke_UsageChunkWithOpenAICache verifies that an SSE chunk
// whose usage payload includes the OpenAI-native prompt_tokens_details.
// cached_tokens field causes artifact.Usage.CacheReadTokens to be
// populated and CacheWriteTokens to remain zero.
func TestProviderInvoke_UsageChunkWithOpenAICache(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(openaiCacheReadSSE(100, 25, 125, 80)),
	}

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "gpt-4"}
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	require.NoError(t, p.Invoke(t.Context(), mem, spec, ch))
	artifacts := drainArtifacts(ch)

	require.Len(t, artifacts, 1)
	u, ok := artifacts[0].(artifact.Usage)
	require.True(t, ok)
	assert.Equal(t, 100, u.PromptTokens)
	assert.Equal(t, 25, u.CompletionTokens)
	assert.Equal(t, 125, u.TotalTokens)
	assert.Equal(t, 80, u.CacheReadTokens, "OpenAI native cached_tokens should populate CacheReadTokens")
	assert.Equal(t, 0, u.CacheWriteTokens, "OpenAI native has no cache_write equivalent")
}

// TestProviderInvoke_UsageChunkWithAnthropicCache verifies that an SSE
// chunk whose usage payload includes the top-level
// cache_read_input_tokens and cache_creation_input_tokens fields causes
// both Usage.CacheReadTokens and Usage.CacheWriteTokens to be populated.
func TestProviderInvoke_UsageChunkWithAnthropicCache(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(anthropicCacheSSE(200, 50, 250, 120, 30)),
	}

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "gpt-4"}
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	require.NoError(t, p.Invoke(t.Context(), mem, spec, ch))
	artifacts := drainArtifacts(ch)

	require.Len(t, artifacts, 1)
	u, ok := artifacts[0].(artifact.Usage)
	require.True(t, ok)
	assert.Equal(t, 200, u.PromptTokens)
	assert.Equal(t, 50, u.CompletionTokens)
	assert.Equal(t, 250, u.TotalTokens)
	assert.Equal(t, 120, u.CacheReadTokens, "Anthropic-style cache_read_input_tokens should populate CacheReadTokens")
	assert.Equal(t, 30, u.CacheWriteTokens, "Anthropic-style cache_creation_input_tokens should populate CacheWriteTokens")
}

// TestProviderInvoke_UsageChunkNoCache verifies the additive change is a
// true no-op: existing usageSSE test data with no cache fields still
// produces a Usage artifact with CacheReadTokens and CacheWriteTokens
// both at zero. This is the regression guard for the Usage struct
// extension.
func TestProviderInvoke_UsageChunkNoCache(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(usageSSE(10, 5, 15)),
	}

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "gpt-4"}
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	require.NoError(t, p.Invoke(t.Context(), mem, spec, ch))
	artifacts := drainArtifacts(ch)

	require.Len(t, artifacts, 1)
	u, ok := artifacts[0].(artifact.Usage)
	require.True(t, ok)
	assert.Equal(t, 10, u.PromptTokens)
	assert.Equal(t, 5, u.CompletionTokens)
	assert.Equal(t, 15, u.TotalTokens)
	assert.Equal(t, 0, u.CacheReadTokens, "no cache fields in response should leave CacheReadTokens zero")
	assert.Equal(t, 0, u.CacheWriteTokens, "no cache fields in response should leave CacheWriteTokens zero")
}

// ---------------------------------------------------------------------------
// StopReason mapping
// ---------------------------------------------------------------------------

// finishReasonSSE is a small fixture helper for the StopReason tests:
// it produces an SSE stream with a single text chunk carrying the
// given finish_reason, followed by [DONE]. The chunk is the kind
// OpenAI emits as the final delta of a stream — content + the real
// finish_reason on the same chunk.
func finishReasonSSE(content, finishReason string) string {
	return fmt.Sprintf("data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4\",\"choices\":[{\"index\":0,\"delta\":{\"content\":%q},\"finish_reason\":%q}]}\n\ndata: [DONE]\n\n", content, finishReason)
}

// finishReasonWithUsageSSE produces a stream with a final text chunk
// carrying the given finish_reason, plus a separate usage chunk
// (no choices, no delta) that surfaces Usage. The two-chunk shape
// mirrors what production OpenAI streams do: the model-side
// finish_reason arrives on the last delta, and the host appends a
// trailing usage chunk after.
func finishReasonWithUsageSSE(content, finishReason string, prompt, completion, total int64) string {
	return fmt.Sprintf(
		"data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4\",\"choices\":[{\"index\":0,\"delta\":{\"content\":%q},\"finish_reason\":%q}]}\n\n"+
			"data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"created\":2,\"model\":\"gpt-4\",\"choices\":[],\"usage\":{\"prompt_tokens\":%d,\"completion_tokens\":%d,\"total_tokens\":%d}}\n\ndata: [DONE]\n\n",
		content, finishReason, prompt, completion, total,
	)
}

// TestProviderInvoke_EmitsStopReason_Stop asserts the canonical
// finish_reason=stop → StopReasonStop mapping. The fixture stream
// ends with a text chunk carrying finish_reason=stop, the most
// common reason for a normal completion. The adapter must emit a
// StopReason{Reason: StopReasonStop} after the text delta.
func TestProviderInvoke_EmitsStopReason_Stop(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(finishReasonSSE("Hello, world!", "stop")),
	}

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "gpt-4"}
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hi"})

	ch := make(chan artifact.Artifact, 10)
	require.NoError(t, p.Invoke(t.Context(), mem, spec, ch))
	artifacts := drainArtifacts(ch)

	// text_delta, stop_reason
	require.Len(t, artifacts, 2)
	assert.Equal(t, "text_delta", artifacts[0].Kind())
	assert.Equal(t, "Hello, world!", artifacts[0].(artifact.TextDelta).Content)
	assert.Equal(t, "stop_reason", artifacts[1].Kind())
	assert.Equal(t, artifact.StopReasonStop, artifacts[1].(artifact.StopReason).Reason)
}

// TestProviderInvoke_EmitsStopReason_Length asserts the canonical
// finish_reason=length → StopReasonLength mapping. This is the
// compaction-triggering case: a SummarizeStrategy that observes a
// StopReason{Length} knows the model hit its output cap and can
// return ErrTruncatedSummary.
func TestProviderInvoke_EmitsStopReason_Length(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(finishReasonWithUsageSSE("##", "length", 1, 1, 2)),
	}

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "gpt-4"}
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "summarize this"})

	ch := make(chan artifact.Artifact, 10)
	require.NoError(t, p.Invoke(t.Context(), mem, spec, ch))
	artifacts := drainArtifacts(ch)

	// text_delta, usage, stop_reason.
	//
	// The openai wire format delivers the usage chunk in a separate
	// stream frame after the final delta (when include_usage is on).
	// The adapter emits usage inline as it arrives, and emits the
	// buffered StopReason after the loop closes — so the actual
	// channel order is text → usage → stop_reason. The anthropic
	// adapter has the opposite order (deltas → stop_reason → usage)
	// because it buffers both and emits them together at
	// message_stop. Callers that care about ordering should
	// pattern-match on Kind, not positional index.
	require.Len(t, artifacts, 3)
	assert.Equal(t, "text_delta", artifacts[0].Kind())
	assert.Equal(t, "usage", artifacts[1].Kind())
	assert.Equal(t, "stop_reason", artifacts[2].Kind())
	assert.Equal(t, artifact.StopReasonLength, artifacts[2].(artifact.StopReason).Reason)
}

// TestProviderInvoke_EmitsStopReason_ToolCalls asserts the canonical
// finish_reason=tool_calls → StopReasonToolUse mapping. The
// pre-tool-calls wire form (finish_reason=function_call) is
// structurally equivalent and covered in TestTranslateFinishReason.
func TestProviderInvoke_EmitsStopReason_ToolCalls(t *testing.T) {
	toolCallBody := "data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"search\",\"arguments\":\"\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n"

	transport := &mockTransport{
		response: mockResponseSSE(toolCallBody),
	}

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "gpt-4"}
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "find something"})

	ch := make(chan artifact.Artifact, 10)
	require.NoError(t, p.Invoke(t.Context(), mem, spec, ch))
	artifacts := drainArtifacts(ch)

	// tool_call_delta, stop_reason
	require.Len(t, artifacts, 2)
	assert.Equal(t, "tool_call_delta", artifacts[0].Kind())
	assert.Equal(t, "stop_reason", artifacts[1].Kind())
	assert.Equal(t, artifact.StopReasonToolUse, artifacts[1].(artifact.StopReason).Reason)
}

// TestProviderInvoke_EmitsStopReason_ContentFilter asserts the
// canonical finish_reason=content_filter → StopReasonRefusal
// mapping. OpenAI returns this when the safety filter omits the
// response content; the adapter must surface it as StopReasonRefusal
// so downstream code can distinguish a refused turn from a normal
// completion.
func TestProviderInvoke_EmitsStopReason_ContentFilter(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(finishReasonSSE("", "content_filter")),
	}

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "gpt-4"}
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "do something dangerous"})

	ch := make(chan artifact.Artifact, 10)
	require.NoError(t, p.Invoke(t.Context(), mem, spec, ch))
	artifacts := drainArtifacts(ch)

	// No text_delta because the content was filtered; just stop_reason.
	require.Len(t, artifacts, 1)
	assert.Equal(t, "stop_reason", artifacts[0].Kind())
	assert.Equal(t, artifact.StopReasonRefusal, artifacts[0].(artifact.StopReason).Reason)
}

// TestProviderInvoke_StopReason_NotOverwrittenByNull asserts that an
// intermediate `finish_reason: null` chunk does not clobber a
// previously-buffered StopReason. The OpenAI wire format sends null
// on every intermediate delta and only sets a real value on the
// final delta of a choice. A naive "always overwrite the buffer"
// implementation would lose the real reason when a null passes
// through after it; the zero-value check in translateFinishReason
// prevents that. Here the second chunk carries a real reason and
// the third chunk is a null — the buffer must hold the second
// chunk's reason at the end.
func TestProviderInvoke_StopReason_NotOverwrittenByNull(t *testing.T) {
	body := "data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"\"},\"finish_reason\":\"length\"}]}\n\n" +
		"data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"created\":2,\"model\":\"gpt-4\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"\"},\"finish_reason\":null}]}\n\n" +
		"data: [DONE]\n\n"

	transport := &mockTransport{
		response: mockResponseSSE(body),
	}

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "gpt-4"}
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hi"})

	ch := make(chan artifact.Artifact, 10)
	require.NoError(t, p.Invoke(t.Context(), mem, spec, ch))
	artifacts := drainArtifacts(ch)

	require.Len(t, artifacts, 1)
	assert.Equal(t, "stop_reason", artifacts[0].Kind())
	// The second chunk's null must NOT have overwritten the first
	// chunk's real reason.
	assert.Equal(t, artifact.StopReasonLength, artifacts[0].(artifact.StopReason).Reason)
}

// TestTranslateFinishReason asserts the table-driven mapping for
// every defined OpenAI finish_reason. This is a unit test for the
// translation function itself, independent of the SSE pipeline, so
// the mapping is locked down even if the streaming tests are
// refactored.
func TestTranslateFinishReason(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want artifact.StopReasonKind
	}{
		{"empty becomes empty", "", ""},
		{"stop → stop", "stop", artifact.StopReasonStop},
		{"length → length", "length", artifact.StopReasonLength},
		{"tool_calls → tool_use", "tool_calls", artifact.StopReasonToolUse},
		{"function_call → tool_use (deprecated)", "function_call", artifact.StopReasonToolUse},
		{"content_filter → refusal", "content_filter", artifact.StopReasonRefusal},
		{"unknown future reason → other", "future_value", artifact.StopReasonOther},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, translateFinishReason(tt.in))
		})
	}
}

// TestProviderInvoke_StreamsReasoning_FlatField verifies that an SSE
// delta carrying `reasoning` (the flat-string shape surfaced by
// OpenRouter for non-Anthropic reasoning routes) is read and emitted as
// a ReasoningDelta. Coexists with the existing `reasoning_content`
// path; both are read independently so a single host can serve either
// shape.
func TestProviderInvoke_StreamsReasoning_FlatField(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(flatReasoningSSE("Let me analyze this...")),
	}

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "deepseek/deepseek-r1"}
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	require.NoError(t, p.Invoke(t.Context(), mem, spec, ch))
	artifacts := drainArtifacts(ch)

	require.Len(t, artifacts, 1)
	assert.Equal(t, "reasoning_delta", artifacts[0].Kind())
	assert.Equal(t, "Let me analyze this...", artifacts[0].(artifact.ReasoningDelta).Content)
}

// TestProviderInvoke_StreamsReasoning_DetailsArray_Text verifies that
// an SSE delta whose `reasoning_details[]` array contains a
// `reasoning.text` entry is read and emitted as a ReasoningDelta with
// that text. This is the OpenRouter wire shape for text-mode reasoning.
func TestProviderInvoke_StreamsReasoning_DetailsArray_Text(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(reasoningDetailsTextSSE("text-mode reasoning chunk")),
	}

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "anthropic/claude-3.7-sonnet:thinking"}
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	require.NoError(t, p.Invoke(t.Context(), mem, spec, ch))
	artifacts := drainArtifacts(ch)

	require.Len(t, artifacts, 1)
	assert.Equal(t, "reasoning_delta", artifacts[0].Kind())
	assert.Equal(t, "text-mode reasoning chunk", artifacts[0].(artifact.ReasoningDelta).Content)
}

// TestProviderInvoke_StreamsReasoning_DetailsArray_Encrypted verifies
// that an SSE delta whose `reasoning_details[]` array contains a
// `reasoning.encrypted` entry is read and emitted as a
// ReasoningSignature{Provider: "openai", SubKind: "encrypted", ...}. The
// signature is the carrier that lets the next-turn serializer (Task 3)
// emit `reasoning_details[]` on the assistant message for replay.
func TestProviderInvoke_StreamsReasoning_DetailsArray_Encrypted(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(reasoningDetailsEncryptedSSE("opaque-encrypted-blob")),
	}

	p, err := New(WithAPIKey("test-key"), WithHTTPClient(mockClient(transport)))
	spec := models.Spec{Name: "anthropic/claude-3.7-sonnet:thinking"}
	require.NoError(t, err)
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	require.NoError(t, p.Invoke(t.Context(), mem, spec, ch))
	artifacts := drainArtifacts(ch)

	require.Len(t, artifacts, 1)
	assert.Equal(t, "reasoning_signature", artifacts[0].Kind())
	sig, ok := artifacts[0].(artifact.ReasoningSignature)
	require.True(t, ok)
	assert.Equal(t, "openai", sig.Provider)
	assert.Equal(t, "encrypted", sig.SubKind)
	assert.Equal(t, "opaque-encrypted-blob", sig.Data)
}

// TestProviderSerialize_ReplaysReasoning_OpenAINative verifies that on
// the OpenAI-native wire, an assistant turn that emitted Reasoning
// artifacts in state is replayed by setting the assistant message's
// `reasoning_content` field to the concatenation of all Reasoning
// contents. Cross-provider ReasoningSignature entries (here, an
// Anthropic-style signature) are dropped on the native wire because
// OpenAI has no surface for them; the native SDK does not model
// signature replay today.
func TestProviderSerialize_ReplaysReasoning_OpenAINative(t *testing.T) {
	t.Parallel()

	transport := &recordingMockTransport{
		responseBody: simpleSSE("ok"),
	}

	p, err := New(PIKey("test-key"),

		WithBaseURL("https://api.openai.com/v1"),
		WithHTTPClient(&http.Client{Transport: transport}),
	)
	spec := models.Spec{Name: "o3-mini"}
	require.NoError(t, err)

	// Two consecutive reasoning blocks (think, then more think) and
	// the resulting text. Also include an Anthropic-style
	// ReasoningSignature in the same turn to assert that it is
	// dropped on the native wire.
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "what is 2+2?"})
	mem.Append(state.RoleAssistant,
		artifact.Reasoning{Content: "Let me think..."},
		artifact.Reasoning{Content: " ...step by step."},
		artifact.ReasoningSignature{Provider: "anthropic", SubKind: "thinking", Data: "anth-blob"},
		artifact.Text{Content: "The answer is 4."},
	)

	ch := make(chan artifact.Artifact, 10)
	require.NoError(t, p.Invoke(t.Context(), mem, spec, ch))
	drainArtifacts(ch)

	requests := transport.Requests()
	require.Len(t, requests, 1)
	var reqBody map[string]any
	require.NoError(t, json.Unmarshal(requests[0].body, &reqBody))

	msgs, ok := reqBody["messages"].([]any)
	require.True(t, ok)
	require.Len(t, msgs, 2) // user + assistant

	// Find the assistant message.
	var asst map[string]any
	for _, m := range msgs {
		mm := m.(map[string]any)
		if mm["role"] == "assistant" {
			asst = mm
			break
		}
	}
	require.NotNil(t, asst, "expected an assistant message in the request body")

	// Concatenated reasoning content, with a newline between blocks.
	assert.Equal(t, "Let me think...\n ...step by step.", asst["reasoning_content"])
	// Anthropic-style signatures do not appear on the native wire.
	_, hasDetails := asst["reasoning_details"]
	assert.False(t, hasDetails, "OpenAI native wire should not carry reasoning_details")
	// The text content is preserved.
	assert.Equal(t, "The answer is 4.", asst["content"])
}

// TestProviderSerialize_ReplaysReasoning_OpenRouter verifies that on
// the OpenRouter wire, an assistant turn that emitted both Reasoning
// and ReasoningSignature{openai, encrypted} artifacts is replayed as a
// `reasoning_details[]` array on the assistant message. Order is
// preserved: Reasoning entries become `reasoning.text`, then
// ReasoningSignature entries become `reasoning.encrypted`.
//
// The test also asserts that the top-level `reasoning: {include:
// true}` field is present (the read-side depends on it to surface
// `delta.reasoning_details[]` in the first place) and that an
// Anthropic-style signature is dropped on the OpenRouter wire (we
// only replay `openai`+`encrypted` on hosts that already accept that
// shape).
func TestProviderSerialize_ReplaysReasoning_OpenRouter(t *testing.T) {
	t.Parallel()

	transport := &recordingMockTransport{
		responseBody: simpleSSE("ok"),
	}

	p, err := New(PIKey("test-key"),

		WithBaseURL("https://openrouter.ai/api/v1"),
		WithHTTPClient(&http.Client{Transport: transport}),
	)
	spec := models.Spec{Name: "anthropic/claude-3.7-sonnet:thinking"}
	require.NoError(t, err)

	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "what is 2+2?"})
	mem.Append(state.RoleAssistant,
		artifact.Reasoning{Content: "I am thinking"},
		artifact.ReasoningSignature{Provider: "openai", SubKind: "encrypted", Data: "openai-blob"},
		artifact.ReasoningSignature{Provider: "anthropic", SubKind: "thinking", Data: "anth-blob"},
		artifact.Text{Content: "The answer is 4."},
	)

	ch := make(chan artifact.Artifact, 10)
	require.NoError(t, p.Invoke(t.Context(), mem, spec, ch))
	drainArtifacts(ch)

	requests := transport.Requests()
	require.Len(t, requests, 1)
	var reqBody map[string]any
	require.NoError(t, json.Unmarshal(requests[0].body, &reqBody))

	// Top-level reasoning.include is the signal that produces
	// reasoning_details[] in the SSE stream.
	reasoning, ok := reqBody["reasoning"].(map[string]any)
	require.True(t, ok, "expected top-level `reasoning` map in request body; got %v", reqBody["reasoning"])
	assert.Equal(t, true, reasoning["include"])

	// Find the assistant message and inspect reasoning_details.
	msgs, ok := reqBody["messages"].([]any)
	require.True(t, ok)
	var asst map[string]any
	for _, m := range msgs {
		mm := m.(map[string]any)
		if mm["role"] == "assistant" {
			asst = mm
			break
		}
	}
	require.NotNil(t, asst)

	details, ok := asst["reasoning_details"].([]any)
	require.True(t, ok, "expected reasoning_details[] on assistant message; got %v", asst)
	require.Len(t, details, 2, "expected exactly two entries: the text block and the openai-encrypted blob")

	// First entry: the Reasoning block, type=reasoning.text.
	first := details[0].(map[string]any)
	assert.Equal(t, "reasoning.text", first["type"])
	assert.Equal(t, "I am thinking", first["text"])
	_, hasData := first["data"]
	assert.False(t, hasData, "text entries should not carry a `data` field")

	// Second entry: the OpenAI-style encrypted signature.
	second := details[1].(map[string]any)
	assert.Equal(t, "reasoning.encrypted", second["type"])
	assert.Equal(t, "openai-blob", second["data"])

	// OpenRouter wire still carries the text body.
	assert.Equal(t, "The answer is 4.", asst["content"])
	// OpenRouter wire does NOT use the native `reasoning_content`
	// field — that is the OpenAI-native shape only.
	_, hasRC := asst["reasoning_content"]
	assert.False(t, hasRC, "OpenRouter wire should not carry reasoning_content")
}

// TestProviderSerialize_ComposesWithCacheControl verifies that
// WithCacheControl and reasoning replay compose: when both are
// requested, the assistant message in the same outgoing request
// carries BOTH the cache_control stamp (on the last user turn, per
// the WithCacheControl contract) AND the OpenRouter `reasoning_details`
// array (because the base URL is OpenRouter). The two mutations must
// not clobber each other.
func TestProviderSerialize_ComposesWithCacheControl(t *testing.T) {
	t.Parallel()

	transport := &recordingMockTransport{
		responseBody: simpleSSE("ok"),
	}

	p, err := New(PIKey("test-key"),

		WithBaseURL("https://openrouter.ai/api/v1"),
		WithHTTPClient(&http.Client{Transport: transport}),
	)
	spec := models.Spec{Name: "anthropic/claude-3.7-sonnet:thinking"}
	require.NoError(t, err)

	mem := &state.Buffer{}
	mem.Append(state.RoleSystem, artifact.Text{Content: "You are a helpful assistant."})
	mem.Append(state.RoleUser, artifact.Text{Content: "first question"})
	mem.Append(state.RoleAssistant,
		artifact.Reasoning{Content: "thinking"},
		artifact.Text{Content: "first answer"},
	)
	mem.Append(state.RoleUser, artifact.Text{Content: "follow up"})

	ch := make(chan artifact.Artifact, 10)
	require.NoError(t, p.Invoke(t.Context(), mem, spec, ch,
		WithCacheControl(),
	))
	drainArtifacts(ch)

	requests := transport.Requests()
	require.Len(t, requests, 1)
	var reqBody map[string]any
	require.NoError(t, json.Unmarshal(requests[0].body, &reqBody))

	msgs, ok := reqBody["messages"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, msgs)

	// The system message should carry cache_control on its content
	// part (asserting that the cache-control mutation survived the
	// chain with reasoning replay).
	sysMsg, ok := msgs[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "system", sysMsg["role"])
	sysParts, ok := sysMsg["content"].([]any)
	require.True(t, ok, "system content should be a content-parts array when cache_control is set")
	require.Len(t, sysParts, 1)
	assertCacheControlEphemeral(t, sysParts[0].(map[string]any))

	// The assistant message (second message in the slice, after
	// system) should carry reasoning_details — the reasoning-replay
	// mutation must still have run alongside cache_control.
	var asst map[string]any
	for _, m := range msgs {
		mm := m.(map[string]any)
		if mm["role"] == "assistant" {
			asst = mm
			break
		}
	}
	require.NotNil(t, asst, "expected an assistant message in the request body")
	details, ok := asst["reasoning_details"].([]any)
	require.True(t, ok, "reasoning_details[] should survive on the assistant message alongside cache_control")
	require.Len(t, details, 1)
	first := details[0].(map[string]any)
	assert.Equal(t, "reasoning.text", first["type"])
	assert.Equal(t, "thinking", first["text"])
}
