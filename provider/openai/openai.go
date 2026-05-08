// Package openai implements a provider adapter for OpenAI-compatible chat
// completions APIs. It wraps the official github.com/openai/openai-go client.
package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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

// Compile-time interface checks.
var _ provider.Provider = (*Provider)(nil)
var _ provider.StreamingProvider = (*Provider)(nil)

// serializeMessages converts tack state into OpenAI chat completion message
// parameters. It concatenates Text artifacts within each turn and maps
// tack roles to OpenAI message types.
func (p *Provider) serializeMessages(s state.State) []openai.ChatCompletionMessageParamUnion {
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

	return messages
}

// Invoke serializes state into an OpenAI chat completions request via the SDK,
// calls the API, and deserializes the response into artifacts.
func (p *Provider) Invoke(ctx context.Context, s state.State) ([]artifact.Artifact, error) {
	messages := p.serializeMessages(s)

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

	msg := resp.Choices[0].Message
	artifacts := []artifact.Artifact{
		artifact.Text{Content: msg.Content},
	}

	if field, ok := msg.JSON.ExtraFields["reasoning_content"]; ok {
		var reasoning string
		if err := json.Unmarshal([]byte(field.Raw()), &reasoning); err == nil && reasoning != "" {
			artifacts = append(artifacts, artifact.Reasoning{Content: reasoning})
		}
	}

	return artifacts, nil
}

// InvokeStreaming serializes state into an OpenAI streaming chat completions
// request, emits partial delta artifacts to deltasCh as they arrive, and
// returns the complete buffered artifacts when the stream finishes.
func (p *Provider) InvokeStreaming(ctx context.Context, s state.State, deltasCh chan<- artifact.Artifact) ([]artifact.Artifact, error) {
	if deltasCh == nil {
		return p.Invoke(ctx, s)
	}

	messages := p.serializeMessages(s)

	stream := p.client.Chat.Completions.NewStreaming(ctx, openai.ChatCompletionNewParams{
		Model:    openai.ChatModel(p.model),
		Messages: messages,
	})

	var textContent strings.Builder

	for stream.Next() {
		chunk := stream.Current()
		if len(chunk.Choices) == 0 {
			continue
		}

		delta := chunk.Choices[0].Delta
		if delta.Content != "" {
			textContent.WriteString(delta.Content)
			select {
			case deltasCh <- artifact.TextDelta{Content: delta.Content}:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}

	if err := stream.Err(); err != nil {
		return nil, fmt.Errorf("streaming chat completion: %w", err)
	}

	var artifacts []artifact.Artifact
	if textContent.Len() > 0 {
		artifacts = append(artifacts, artifact.Text{Content: textContent.String()})
	}

	return artifacts, nil
}
