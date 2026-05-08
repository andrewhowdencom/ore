// Package openai implements a provider adapter for OpenAI-compatible chat
// completions APIs. It wraps the official github.com/openai/openai-go client.
package openai

import (
	"context"
	"fmt"

	"github.com/andrewhowdencom/tack/artifact"
	"github.com/andrewhowdencom/tack/provider"
	"github.com/andrewhowdencom/tack/state"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

// Provider implements provider.Provider for OpenAI-compatible APIs using the
// official OpenAI Go SDK.
type Provider struct {
	client openai.Client
	model  string
}

// config holds the build-time configuration for the Provider.
type config struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient option.HTTPClient
}

// Option configures a Provider via the functional options pattern.
type Option func(*config)

// WithBaseURL sets a custom API base URL (e.g., for local proxies).
func WithBaseURL(url string) Option {
	return func(c *config) {
		c.baseURL = url
	}
}

// WithHTTPClient sets a custom HTTP client for the provider. This is primarily
// useful for testing.
func WithHTTPClient(client option.HTTPClient) Option {
	return func(c *config) {
		c.httpClient = client
	}
}

// New creates an OpenAI-compatible provider.
func New(apiKey, model string, opts ...Option) *Provider {
	cfg := &config{
		apiKey: apiKey,
		model:  model,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	sdkOpts := []option.RequestOption{option.WithAPIKey(cfg.apiKey)}
	if cfg.baseURL != "" {
		sdkOpts = append(sdkOpts, option.WithBaseURL(cfg.baseURL))
	}
	if cfg.httpClient != nil {
		sdkOpts = append(sdkOpts, option.WithHTTPClient(cfg.httpClient))
	}

	return &Provider{
		client: openai.NewClient(sdkOpts...),
		model:  cfg.model,
	}
}

// Compile-time interface check.
var _ provider.Provider = (*Provider)(nil)

// Invoke serializes state into an OpenAI chat completions request via the SDK,
// calls the API, and deserializes the response into artifacts.
func (p *Provider) Invoke(ctx context.Context, s state.State) ([]artifact.Artifact, error) {
	turns := s.Turns()
	messages := make([]openai.ChatCompletionMessageParamUnion, 0, len(turns))

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

		var msg openai.ChatCompletionMessageParamUnion
		switch turn.Role {
		case state.RoleSystem:
			msg = openai.SystemMessage(content)
		case state.RoleUser:
			msg = openai.UserMessage(content)
		case state.RoleAssistant:
			msg = openai.AssistantMessage(content)
		default:
			msg = openai.UserMessage(content)
		}
		messages = append(messages, msg)
	}

	resp, err := p.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:    openai.ChatModel(p.model),
		Messages: messages,
	})
	if err != nil {
		return nil, fmt.Errorf("chat completion: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	return []artifact.Artifact{
		artifact.Text{Content: resp.Choices[0].Message.Content},
	}, nil
}
