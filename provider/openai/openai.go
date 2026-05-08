// Package openai implements a provider adapter for OpenAI-compatible chat
// completions APIs. It uses the standard net/http client and supports
// custom base URLs for local proxies or alternative endpoints.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/andrewhowdencom/tack/artifact"
	"github.com/andrewhowdencom/tack/provider"
	"github.com/andrewhowdencom/tack/state"
)

const defaultBaseURL = "https://api.openai.com/v1"

// Provider implements provider.Provider for OpenAI-compatible APIs.
type Provider struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

// Option configures a Provider via the functional options pattern.
type Option func(*Provider)

// WithBaseURL sets a custom API base URL (e.g., for local proxies).
func WithBaseURL(url string) Option {
	return func(p *Provider) {
		p.baseURL = url
	}
}

// WithClient sets a custom HTTP client for the provider.
func WithClient(client *http.Client) Option {
	return func(p *Provider) {
		p.client = client
	}
}

// New creates an OpenAI-compatible provider.
func New(apiKey, model string, opts ...Option) *Provider {
	p := &Provider{
		apiKey:  apiKey,
		model:   model,
		baseURL: defaultBaseURL,
		client:  http.DefaultClient,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Compile-time interface check.
var _ provider.Provider = (*Provider)(nil)

type chatCompletionRequest struct {
	Model    string    `json:"model"`
	Messages []message `json:"messages"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// Invoke serializes state into an OpenAI chat completions request,
// calls the API, and deserializes the response into artifacts.
func (p *Provider) Invoke(ctx context.Context, s state.State) ([]artifact.Artifact, error) {
	turns := s.Turns()
	messages := make([]message, 0, len(turns))

	for _, turn := range turns {
		content := ""
		for _, art := range turn.Artifacts {
			if text, ok := art.(artifact.Text); ok {
				if content != "" {
					content += "\n"
				}
				content += text.Content
			}
			// Non-text artifacts are skipped in this initial implementation.
		}
		messages = append(messages, message{
			Role:    string(turn.Role),
			Content: content,
		})
	}

	reqBody := chatCompletionRequest{
		Model:    p.model,
		Messages: messages,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var result chatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(result.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	return []artifact.Artifact{
		artifact.Text{Content: result.Choices[0].Message.Content},
	}, nil
}
