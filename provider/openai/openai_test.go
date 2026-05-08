package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/andrewhowdencom/tack/artifact"
	"github.com/andrewhowdencom/tack/state"
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
