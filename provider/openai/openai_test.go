package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/state"
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

func multiChunkSSE(contents ...string) string {
	var sb strings.Builder
	for i, content := range contents {
		sb.WriteString(fmt.Sprintf("data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"created\":%d,\"model\":\"gpt-4\",\"choices\":[{\"index\":0,\"delta\":{\"content\":%q},\"finish_reason\":null}]}\n\n", i+1, content))
	}
	sb.WriteString("data: [DONE]\n\n")
	return sb.String()
}

func emptyChoicesSSE() string {
	return "data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"choices\":[]}\n\ndata: [DONE]\n\n"
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

	p := New("test-key", "gpt-4", WithHTTPClient(mockClient(transport)))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	err := p.Invoke(t.Context(), mem, ch)
	require.NoError(t, err)

	artifacts := drainArtifacts(ch)
	// Delta + complete
	require.Len(t, artifacts, 2)
	assert.Equal(t, "text_delta", artifacts[0].Kind())
	assert.Equal(t, "Hello, world!", artifacts[0].(artifact.TextDelta).Content)
	assert.Equal(t, "text", artifacts[1].Kind())
	assert.Equal(t, "Hello, world!", artifacts[1].(artifact.Text).Content)
}

func TestProviderInvoke_HTTPError(t *testing.T) {
	transport := &mockTransport{
		response: mockResponse(401, `{"error":{"message":"invalid key","type":"invalid_request_error"}}`),
	}

	p := New("bad-key", "gpt-4", WithHTTPClient(mockClient(transport)))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	err := p.Invoke(t.Context(), mem, ch)
	require.Error(t, err)
}

func TestProviderInvoke_EmptyChoices(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(emptyChoicesSSE()),
	}

	p := New("test-key", "gpt-4", WithHTTPClient(mockClient(transport)))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	err := p.Invoke(t.Context(), mem, ch)
	require.NoError(t, err)

	artifacts := drainArtifacts(ch)
	assert.Empty(t, artifacts)
}

func TestProviderInvoke_MultipleTextArtifacts(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(simpleSSE("ok")),
	}

	p := New("test-key", "gpt-4", WithHTTPClient(mockClient(transport)))
	mem := &state.Memory{}
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

	p := New("test-key", "gpt-4", WithHTTPClient(mockClient(transport)))
	mem := &state.Memory{}
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

	p := New("test-key", "gpt-4", WithHTTPClient(mockClient(transport)))
	mem := &state.Memory{}

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

	p := New("test-key", "gpt-4", WithHTTPClient(mockClient(transport)))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	err := p.Invoke(t.Context(), mem, ch)
	require.NoError(t, err)

	artifacts := drainArtifacts(ch)
	require.Len(t, artifacts, 2)
	text, ok := artifacts[1].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "first", text.Content)
}

func TestProviderInvoke_MalformedJSON(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE("data: {\"invalid\n\ndata: [DONE]\n\n"),
	}

	p := New("test-key", "gpt-4", WithHTTPClient(mockClient(transport)))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	err := p.Invoke(t.Context(), mem, ch)
	require.Error(t, err)
}

func TestProviderInvoke_ContextCancellation(t *testing.T) {
	transport := &mockTransport{
		err: context.Canceled,
	}

	p := New("test-key", "gpt-4", WithHTTPClient(mockClient(transport)))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ch := make(chan artifact.Artifact, 10)
	err := p.Invoke(ctx, mem, ch)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestProviderInvoke_CustomClient(t *testing.T) {
	wantErr := errors.New("custom transport error")
	transport := &mockTransport{
		err: wantErr,
	}

	p := New("test-key", "gpt-4", WithHTTPClient(mockClient(transport)))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	err := p.Invoke(t.Context(), mem, ch)
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
}

func TestProviderInvoke_WithReasoning(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(reasoningSSE("Hello, world!", "Let me analyze this...")),
	}

	p := New("test-key", "o3-mini", WithHTTPClient(mockClient(transport)))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	err := p.Invoke(t.Context(), mem, ch)
	require.NoError(t, err)

	artifacts := drainArtifacts(ch)
	// text_delta, reasoning_delta, text, reasoning
	require.Len(t, artifacts, 4)
	assert.Equal(t, "text_delta", artifacts[0].Kind())
	assert.Equal(t, "Hello, world!", artifacts[0].(artifact.TextDelta).Content)
	assert.Equal(t, "reasoning_delta", artifacts[1].Kind())
	assert.Equal(t, "Let me analyze this...", artifacts[1].(artifact.ReasoningDelta).Content)
	assert.Equal(t, "text", artifacts[2].Kind())
	assert.Equal(t, "Hello, world!", artifacts[2].(artifact.Text).Content)
	assert.Equal(t, "reasoning", artifacts[3].Kind())
	assert.Equal(t, "Let me analyze this...", artifacts[3].(artifact.Reasoning).Content)
}

func TestProviderInvoke_EmptyReasoning(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE("data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"o3-mini\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hello, world!\",\"reasoning_content\":\"\"},\"finish_reason\":null}]}\n\ndata: [DONE]\n\n"),
	}

	p := New("test-key", "o3-mini", WithHTTPClient(mockClient(transport)))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	err := p.Invoke(t.Context(), mem, ch)
	require.NoError(t, err)

	artifacts := drainArtifacts(ch)
	// text_delta, text (empty reasoning is skipped)
	require.Len(t, artifacts, 2)
	assert.Equal(t, "text_delta", artifacts[0].Kind())
	assert.Equal(t, "text", artifacts[1].Kind())
}

func TestProviderInvoke_ReasoningOnly(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(reasoningOnlySSE("Let me analyze", " this request")),
	}

	p := New("test-key", "o3-mini", WithHTTPClient(mockClient(transport)))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	err := p.Invoke(t.Context(), mem, ch)
	require.NoError(t, err)

	artifacts := drainArtifacts(ch)
	// reasoning_delta x2, reasoning
	require.Len(t, artifacts, 3)
	assert.Equal(t, "reasoning_delta", artifacts[0].Kind())
	assert.Equal(t, "Let me analyze", artifacts[0].(artifact.ReasoningDelta).Content)
	assert.Equal(t, "reasoning_delta", artifacts[1].Kind())
	assert.Equal(t, " this request", artifacts[1].(artifact.ReasoningDelta).Content)
	assert.Equal(t, "reasoning", artifacts[2].Kind())
	assert.Equal(t, "Let me analyze this request", artifacts[2].(artifact.Reasoning).Content)
}

func TestProviderInvoke_RoleMapping(t *testing.T) {
	transport := &mockTransport{
		response: mockResponseSSE(simpleSSE("ok")),
	}

	p := New("test-key", "gpt-4", WithHTTPClient(mockClient(transport)))
	mem := &state.Memory{}
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

	p := New("test-key", "gpt-4", WithHTTPClient(&http.Client{Transport: transport}))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			tools := []provider.Tool{{Name: fmt.Sprintf("tool-%d", idx), Description: "test", Schema: map[string]any{"type": "object"}}}
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

	p := New("test-key", "gpt-4", WithHTTPClient(mockClient(transport)))
	mem := &state.Memory{}
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

			p := New("test-key", "o3-mini", WithHTTPClient(mockClient(transport)))
			mem := &state.Memory{}
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

	p := New("test-key", "gpt-4", WithHTTPClient(mockClient(transport)))
	mem := &state.Memory{}
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

	tools := []provider.Tool{
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

	p := New("test-key", "gpt-4", WithHTTPClient(mockClient(transport)))
	mem := &state.Memory{}
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
