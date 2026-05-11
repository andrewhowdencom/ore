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

func TestProviderInvoke_Success(t *testing.T) {
	transport := &mockTransport{
		response: mockResponse(200, `{"choices":[{"message":{"role":"assistant","content":"Hello, world!"}}]}`),
	}

	p := New("test-key", "gpt-4", WithHTTPClient(mockClient(transport)))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	artifacts, err := p.Invoke(t.Context(), mem)
	require.NoError(t, err)
	require.Len(t, artifacts, 1)

	text, ok := artifacts[0].(artifact.Text)
	require.True(t, ok, "expected artifact.Text, got %T", artifacts[0])
	assert.Equal(t, "Hello, world!", text.Content)
}

func TestProviderInvoke_HTTPError(t *testing.T) {
	transport := &mockTransport{
		response: mockResponse(401, `{"error":{"message":"invalid key","type":"invalid_request_error"}}`),
	}

	p := New("bad-key", "gpt-4", WithHTTPClient(mockClient(transport)))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	_, err := p.Invoke(t.Context(), mem)
	require.Error(t, err)
}

func TestProviderInvoke_EmptyChoices(t *testing.T) {
	transport := &mockTransport{
		response: mockResponse(200, `{"choices":[]}`),
	}

	p := New("test-key", "gpt-4", WithHTTPClient(mockClient(transport)))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	_, err := p.Invoke(t.Context(), mem)
	require.Error(t, err)
	assert.Equal(t, "no choices in response", err.Error())
}

func TestProviderInvoke_MultipleTextArtifacts(t *testing.T) {
	transport := &mockTransport{
		response: mockResponse(200, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`),
	}

	p := New("test-key", "gpt-4", WithHTTPClient(mockClient(transport)))
	mem := &state.Memory{}
	mem.Append(state.RoleUser,
		artifact.Text{Content: "line1"},
		artifact.Text{Content: "line2"},
	)

	_, err := p.Invoke(t.Context(), mem)
	require.NoError(t, err)

	// Verify the request body contains concatenated text.
	require.NotNil(t, transport.request)
	body, _ := io.ReadAll(transport.request.Body)
	assert.Contains(t, string(body), "line1\\nline2")
}

func TestProviderInvoke_NonTextArtifactsSkipped(t *testing.T) {
	transport := &mockTransport{
		response: mockResponse(200, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`),
	}

	p := New("test-key", "gpt-4", WithHTTPClient(mockClient(transport)))
	mem := &state.Memory{}
	mem.Append(state.RoleUser,
		artifact.Text{Content: "hello"},
		artifact.ToolCall{Name: "foo", Arguments: "{}"},
		artifact.Image{URL: "http://example.com/img.png"},
	)

	_, err := p.Invoke(t.Context(), mem)
	require.NoError(t, err)

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
		response: mockResponse(200, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`),
	}

	p := New("test-key", "gpt-4", WithHTTPClient(mockClient(transport)))
	mem := &state.Memory{}

	_, err := p.Invoke(t.Context(), mem)
	require.NoError(t, err)

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
		response: mockResponse(200, `{"choices":[{"message":{"role":"assistant","content":"first"}},{"message":{"role":"assistant","content":"second"}}]}`),
	}

	p := New("test-key", "gpt-4", WithHTTPClient(mockClient(transport)))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	artifacts, err := p.Invoke(t.Context(), mem)
	require.NoError(t, err)
	require.Len(t, artifacts, 1)

	text, ok := artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "first", text.Content)
}

func TestProviderInvoke_MalformedJSON(t *testing.T) {
	transport := &mockTransport{
		response: mockResponse(200, `{"invalid`),
	}

	p := New("test-key", "gpt-4", WithHTTPClient(mockClient(transport)))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	_, err := p.Invoke(t.Context(), mem)
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

	_, err := p.Invoke(ctx, mem)
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

	_, err := p.Invoke(t.Context(), mem)
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
}

func TestProviderInvoke_WithReasoning(t *testing.T) {
	transport := &mockTransport{
		response: mockResponse(200, `{"choices":[{"message":{"role":"assistant","content":"Hello, world!","reasoning_content":"Let me analyze this..."}}]}`),
	}

	p := New("test-key", "o3-mini", WithHTTPClient(mockClient(transport)))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	artifacts, err := p.Invoke(t.Context(), mem)
	require.NoError(t, err)
	require.Len(t, artifacts, 2)

	text, ok := artifacts[0].(artifact.Text)
	require.True(t, ok, "expected artifact.Text, got %T", artifacts[0])
	assert.Equal(t, "Hello, world!", text.Content)

	reasoning, ok := artifacts[1].(artifact.Reasoning)
	require.True(t, ok, "expected artifact.Reasoning, got %T", artifacts[1])
	assert.Equal(t, "Let me analyze this...", reasoning.Content)
}

func TestProviderInvoke_EmptyReasoning(t *testing.T) {
	transport := &mockTransport{
		response: mockResponse(200, `{"choices":[{"message":{"role":"assistant","content":"Hello, world!","reasoning_content":""}}]}`),
	}

	p := New("test-key", "o3-mini", WithHTTPClient(mockClient(transport)))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	artifacts, err := p.Invoke(t.Context(), mem)
	require.NoError(t, err)
	require.Len(t, artifacts, 1)

	text, ok := artifacts[0].(artifact.Text)
	require.True(t, ok, "expected artifact.Text, got %T", artifacts[0])
	assert.Equal(t, "Hello, world!", text.Content)
}

func TestProviderInvoke_RoleMapping(t *testing.T) {
	transport := &mockTransport{
		response: mockResponse(200, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`),
	}

	p := New("test-key", "gpt-4", WithHTTPClient(mockClient(transport)))
	mem := &state.Memory{}
	mem.Append(state.RoleSystem, artifact.Text{Content: "sys"})
	mem.Append(state.RoleUser, artifact.Text{Content: "usr"})
	mem.Append(state.RoleAssistant, artifact.Text{Content: "asst"})
	mem.Append(state.RoleTool, artifact.Text{Content: "tool"})

	_, err := p.Invoke(t.Context(), mem)
	require.NoError(t, err)

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

func TestProviderInvokeStreaming_Success(t *testing.T) {
	sseBody := "data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"created\":1693583820,\"model\":\"gpt-4\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hello\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"created\":1693583820,\"model\":\"gpt-4\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\" world\"},\"finish_reason\":null}]}\n\n" +
		"data: [DONE]\n\n"

	resp := &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(sseBody)),
	}

	transport := &mockTransport{response: resp}
	p := New("test-key", "gpt-4", WithHTTPClient(mockClient(transport)))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	artifacts, err := p.InvokeStreaming(t.Context(), mem, ch)
	require.NoError(t, err)

	// Verify deltas were emitted.
	require.Len(t, ch, 2)
	d1 := <-ch
	assert.Equal(t, "text_delta", d1.Kind())
	assert.Equal(t, "Hello", d1.(artifact.TextDelta).Content)
	d2 := <-ch
	assert.Equal(t, "text_delta", d2.Kind())
	assert.Equal(t, " world", d2.(artifact.TextDelta).Content)

	// Verify complete artifact.
	require.Len(t, artifacts, 1)
	text, ok := artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "Hello world", text.Content)
}

func TestProviderInvokeStreaming_NilChannel(t *testing.T) {
	transport := &mockTransport{
		response: mockResponse(200, `{"choices":[{"message":{"role":"assistant","content":"Hello!"}}]}`),
	}

	p := New("test-key", "gpt-4", WithHTTPClient(mockClient(transport)))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	// Passing nil channel should fall back to non-streaming Invoke.
	artifacts, err := p.InvokeStreaming(t.Context(), mem, nil)
	require.NoError(t, err)
	require.Len(t, artifacts, 1)
	text, ok := artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "Hello!", text.Content)
}

func TestProviderInvokeStreaming_WithReasoning(t *testing.T) {
	sseBody := "data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"created\":1693583820,\"model\":\"o3-mini\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hello\",\"reasoning_content\":\"Let me think\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"created\":1693583820,\"model\":\"o3-mini\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\" world\",\"reasoning_content\":\" about this\"},\"finish_reason\":null}]}\n\n" +
		"data: [DONE]\n\n"

	resp := &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(sseBody)),
	}

	transport := &mockTransport{response: resp}
	p := New("test-key", "o3-mini", WithHTTPClient(mockClient(transport)))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	artifacts, err := p.InvokeStreaming(t.Context(), mem, ch)
	require.NoError(t, err)

	// Verify deltas were emitted.
	require.Len(t, ch, 4)
	d1 := <-ch
	assert.Equal(t, "text_delta", d1.Kind())
	assert.Equal(t, "Hello", d1.(artifact.TextDelta).Content)
	d2 := <-ch
	assert.Equal(t, "reasoning_delta", d2.Kind())
	assert.Equal(t, "Let me think", d2.(artifact.ReasoningDelta).Content)
	d3 := <-ch
	assert.Equal(t, "text_delta", d3.Kind())
	assert.Equal(t, " world", d3.(artifact.TextDelta).Content)
	d4 := <-ch
	assert.Equal(t, "reasoning_delta", d4.Kind())
	assert.Equal(t, " about this", d4.(artifact.ReasoningDelta).Content)

	// Verify complete artifacts.
	require.Len(t, artifacts, 2)
	text, ok := artifacts[0].(artifact.Text)
	require.True(t, ok, "expected artifact.Text, got %T", artifacts[0])
	assert.Equal(t, "Hello world", text.Content)

	reasoning, ok := artifacts[1].(artifact.Reasoning)
	require.True(t, ok, "expected artifact.Reasoning, got %T", artifacts[1])
	assert.Equal(t, "Let me think about this", reasoning.Content)
}

func TestProviderInvokeStreaming_ReasoningOnly(t *testing.T) {
	sseBody := "data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"created\":1693583820,\"model\":\"o3-mini\",\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"Let me analyze\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"chatcmpl-test\",\"object\":\"chat.completion.chunk\",\"created\":1693583820,\"model\":\"o3-mini\",\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\" this request\"},\"finish_reason\":null}]}\n\n" +
		"data: [DONE]\n\n"

	resp := &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(sseBody)),
	}

	transport := &mockTransport{response: resp}
	p := New("test-key", "o3-mini", WithHTTPClient(mockClient(transport)))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)
	artifacts, err := p.InvokeStreaming(t.Context(), mem, ch)
	require.NoError(t, err)

	// Verify deltas were emitted.
	require.Len(t, ch, 2)
	d1 := <-ch
	assert.Equal(t, "reasoning_delta", d1.Kind())
	assert.Equal(t, "Let me analyze", d1.(artifact.ReasoningDelta).Content)
	d2 := <-ch
	assert.Equal(t, "reasoning_delta", d2.Kind())
	assert.Equal(t, " this request", d2.(artifact.ReasoningDelta).Content)

	// Verify complete artifacts.
	require.Len(t, artifacts, 1)
	reasoning, ok := artifacts[0].(artifact.Reasoning)
	require.True(t, ok, "expected artifact.Reasoning, got %T", artifacts[0])
	assert.Equal(t, "Let me analyze this request", reasoning.Content)
}

func TestProviderInvoke_ConcurrentSetTools(t *testing.T) {
	transport := &concurrentMockTransport{
		responseBody: `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`,
	}

	p := New("test-key", "gpt-4", WithHTTPClient(&http.Client{Transport: transport}))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	var wg sync.WaitGroup

	// Goroutine A: continuously updates tools.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_ = p.SetTools([]provider.Tool{
				{Name: fmt.Sprintf("tool-%d", i), Description: "test", Schema: map[string]any{"type": "object"}},
			})
		}
	}()

	// Goroutine B: continuously invokes the provider.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_, _ = p.Invoke(t.Context(), mem)
		}
	}()

	wg.Wait()
}

func TestProviderInvoke_MixedAssistantTextAndToolCalls(t *testing.T) {
	transport := &mockTransport{
		response: mockResponse(200, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`),
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

	_, err := p.Invoke(t.Context(), mem)
	require.NoError(t, err)

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
		response: mockResponse(200, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`),
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
	require.NoError(t, p.SetTools(tools))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	_, err := p.Invoke(t.Context(), mem)
	require.NoError(t, err)

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
