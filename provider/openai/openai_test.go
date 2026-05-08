package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/andrewhowdencom/tack/artifact"
	"github.com/andrewhowdencom/tack/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderInvoke_Success(t *testing.T) {
	wantContent := "Hello, world!"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))

		var reqBody chatCompletionRequest
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&reqBody))
		assert.Equal(t, "gpt-4", reqBody.Model)
		assert.Len(t, reqBody.Messages, 1)
		assert.Equal(t, "user", reqBody.Messages[0].Role)
		assert.Equal(t, "hello", reqBody.Messages[0].Content)

		resp := chatCompletionResponse{
			Choices: []struct {
				Message struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"message"`
			}{
				{
					Message: struct {
						Role    string `json:"role"`
						Content string `json:"content"`
					}{
						Role:    "assistant",
						Content: wantContent,
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		assert.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
	defer server.Close()

	p := New("test-key", "gpt-4", WithBaseURL(server.URL))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	artifacts, err := p.Invoke(t.Context(), mem)
	require.NoError(t, err)
	require.Len(t, artifacts, 1)

	text, ok := artifacts[0].(artifact.Text)
	require.True(t, ok, "expected artifact.Text, got %T", artifacts[0])
	assert.Equal(t, wantContent, text.Content)
}

func TestProviderInvoke_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, err := io.WriteString(w, `{"error": "invalid key"}`)
		assert.NoError(t, err)
	}))
	defer server.Close()

	p := New("bad-key", "gpt-4", WithBaseURL(server.URL))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	_, err := p.Invoke(t.Context(), mem)
	require.Error(t, err)
}

func TestProviderInvoke_EmptyChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := chatCompletionResponse{Choices: []struct {
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
		}{}}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		assert.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
	defer server.Close()

	p := New("test-key", "gpt-4", WithBaseURL(server.URL))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	_, err := p.Invoke(t.Context(), mem)
	require.Error(t, err)
	assert.Equal(t, "no choices in response", err.Error())
}

func TestProviderInvoke_MultipleTextArtifacts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody chatCompletionRequest
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&reqBody))

		require.Len(t, reqBody.Messages, 1)
		assert.Equal(t, "line1\nline2", reqBody.Messages[0].Content)

		resp := chatCompletionResponse{
			Choices: []struct {
				Message struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Message: struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				}{Role: "assistant", Content: "ok"}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		assert.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
	defer server.Close()

	p := New("test-key", "gpt-4", WithBaseURL(server.URL))
	mem := &state.Memory{}
	mem.Append(state.RoleUser,
		artifact.Text{Content: "line1"},
		artifact.Text{Content: "line2"},
	)

	_, err := p.Invoke(t.Context(), mem)
	require.NoError(t, err)
}

func TestProviderInvoke_NonTextArtifactsSkipped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody chatCompletionRequest
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&reqBody))

		require.Len(t, reqBody.Messages, 1)
		// Only text artifacts should be serialized; ToolCall and Image skipped.
		assert.Equal(t, "hello", reqBody.Messages[0].Content)

		resp := chatCompletionResponse{
			Choices: []struct {
				Message struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Message: struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				}{Role: "assistant", Content: "ok"}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		assert.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
	defer server.Close()

	p := New("test-key", "gpt-4", WithBaseURL(server.URL))
	mem := &state.Memory{}
	mem.Append(state.RoleUser,
		artifact.Text{Content: "hello"},
		artifact.ToolCall{Name: "foo", Arguments: "{}"},
		artifact.Image{URL: "http://example.com/img.png"},
	)

	_, err := p.Invoke(t.Context(), mem)
	require.NoError(t, err)
}

func TestProviderInvoke_EmptyState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody chatCompletionRequest
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&reqBody))
		assert.Empty(t, reqBody.Messages)

		resp := chatCompletionResponse{
			Choices: []struct {
				Message struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Message: struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				}{Role: "assistant", Content: "ok"}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		assert.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
	defer server.Close()

	p := New("test-key", "gpt-4", WithBaseURL(server.URL))
	mem := &state.Memory{}

	_, err := p.Invoke(t.Context(), mem)
	require.NoError(t, err)
}

func TestProviderInvoke_MultipleChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := chatCompletionResponse{
			Choices: []struct {
				Message struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Message: struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				}{Role: "assistant", Content: "first"}},
				{Message: struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				}{Role: "assistant", Content: "second"}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		assert.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
	defer server.Close()

	p := New("test-key", "gpt-4", WithBaseURL(server.URL))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	artifacts, err := p.Invoke(t.Context(), mem)
	require.NoError(t, err)
	require.Len(t, artifacts, 1)

	text, ok := artifacts[0].(artifact.Text)
	require.True(t, ok, "expected artifact.Text, got %T", artifacts[0])
	assert.Equal(t, "first", text.Content)
}

func TestProviderInvoke_MalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err := io.WriteString(w, `{"invalid`)
		assert.NoError(t, err)
	}))
	defer server.Close()

	p := New("test-key", "gpt-4", WithBaseURL(server.URL))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	_, err := p.Invoke(t.Context(), mem)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode response")
}

func TestProviderInvoke_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until context is cancelled.
		<-r.Context().Done()
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	p := New("test-key", "gpt-4", WithBaseURL(server.URL))
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

	client := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			return nil, wantErr
		}),
	}

	p := New("test-key", "gpt-4", WithClient(client))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	_, err := p.Invoke(t.Context(), mem)
	require.ErrorIs(t, err, wantErr)
}

func TestProviderInvoke_RoleMapping(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody chatCompletionRequest
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&reqBody))
		require.Len(t, reqBody.Messages, 4)

		roles := []string{"system", "user", "assistant", "tool"}
		for i, want := range roles {
			assert.Equal(t, want, reqBody.Messages[i].Role)
		}

		resp := chatCompletionResponse{
			Choices: []struct {
				Message struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Message: struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				}{Role: "assistant", Content: "ok"}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		assert.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
	defer server.Close()

	p := New("test-key", "gpt-4", WithBaseURL(server.URL))
	mem := &state.Memory{}
	mem.Append(state.RoleSystem, artifact.Text{Content: "sys"})
	mem.Append(state.RoleUser, artifact.Text{Content: "usr"})
	mem.Append(state.RoleAssistant, artifact.Text{Content: "asst"})
	mem.Append(state.RoleTool, artifact.Text{Content: "tool"})

	_, err := p.Invoke(t.Context(), mem)
	require.NoError(t, err)
}

// roundTripperFunc is a convenience type for creating a custom http.RoundTripper.
type roundTripperFunc func(req *http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
