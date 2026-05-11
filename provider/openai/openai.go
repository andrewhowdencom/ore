// Package openai implements a provider adapter for OpenAI-compatible chat
// completions APIs. It wraps the official github.com/openai/openai-go client.
package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/openai/openai-go/packages/param"
	"github.com/andrewhowdencom/ore/state"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

// Provider implements provider.Provider for OpenAI-compatible APIs using the
// official OpenAI Go SDK.
type Provider struct {
	client openai.Client
	model  string
}

// toolOption is a per-invocation option that configures available tools.
type toolOption struct {
	tools []provider.Tool
}

func (toolOption) IsInvokeOption() {}

// WithTools returns an InvokeOption that configures the set of available tools
// for a single provider invocation.
func WithTools(tools []provider.Tool) provider.InvokeOption {
	return toolOption{tools: tools}
}

// temperatureOption is a per-invocation option that sets the sampling temperature.
type temperatureOption struct {
	t float64
}

func (temperatureOption) IsInvokeOption() {}

// WithTemperature returns an InvokeOption that sets the sampling temperature
// for a single provider invocation.
func WithTemperature(t float64) provider.InvokeOption {
	return temperatureOption{t: t}
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

// serializeMessages converts ore state into OpenAI chat completion message
// parameters. It maps ore roles to OpenAI message types and preserves
// ToolCall and ToolResult artifacts for tool calling conversations.
func (p *Provider) serializeMessages(s state.State) []openai.ChatCompletionMessageParamUnion {
	turns := s.Turns()
	messages := make([]openai.ChatCompletionMessageParamUnion, 0, len(turns))

	for _, turn := range turns {
		switch turn.Role {
		case state.RoleSystem:
			content := concatText(turn.Artifacts)
			messages = append(messages, openai.SystemMessage(content))
		case state.RoleUser:
			content := concatText(turn.Artifacts)
			messages = append(messages, openai.UserMessage(content))
		case state.RoleAssistant:
			var toolCalls []artifact.ToolCall
			var textContent string
			for _, art := range turn.Artifacts {
				switch a := art.(type) {
				case artifact.Text:
					if textContent != "" {
						textContent += "\n"
					}
					textContent += a.Content
				case artifact.ToolCall:
					toolCalls = append(toolCalls, a)
				}
			}

			if len(toolCalls) > 0 {
				tcParams := make([]openai.ChatCompletionMessageToolCallParam, len(toolCalls))
				for i, tc := range toolCalls {
					tcParams[i] = openai.ChatCompletionMessageToolCallParam{
						ID: tc.ID,
						Function: openai.ChatCompletionMessageToolCallFunctionParam{
							Name:      tc.Name,
							Arguments: tc.Arguments,
						},
					}
				}
				assistantMsg := openai.ChatCompletionAssistantMessageParam{
					ToolCalls: tcParams,
				}
				if textContent != "" {
					assistantMsg.Content = openai.ChatCompletionAssistantMessageParamContentUnion{
						OfString: param.NewOpt(textContent),
					}
				}
				messages = append(messages, openai.ChatCompletionMessageParamUnion{
					OfAssistant: &assistantMsg,
				})
			} else {
				messages = append(messages, openai.AssistantMessage(textContent))
			}
		case state.RoleTool:
			var toolMsgs []openai.ChatCompletionMessageParamUnion
			for _, art := range turn.Artifacts {
				if tr, ok := art.(artifact.ToolResult); ok {
					toolMsgs = append(toolMsgs, openai.ToolMessage(tr.Content, tr.ToolCallID))
				}
			}
			if len(toolMsgs) > 0 {
				messages = append(messages, toolMsgs...)
			} else {
				// Fallback: non-ToolResult artifacts in RoleTool turns are treated as
				// user messages for backward compatibility.
				content := concatText(turn.Artifacts)
				messages = append(messages, openai.UserMessage(content))
			}
		default:
			content := concatText(turn.Artifacts)
			messages = append(messages, openai.UserMessage(content))
		}
	}

	return messages
}

// concatText extracts and concatenates Text artifacts from a slice.
func concatText(artifacts []artifact.Artifact) string {
	var content string
	for _, art := range artifacts {
		if text, ok := art.(artifact.Text); ok {
			if content != "" {
				content += "\n"
			}
			content += text.Content
		}
	}
	return content
}

// Invoke serializes state into an OpenAI chat completions request via the SDK,
// calls the API, and deserializes the response into artifacts.
func (p *Provider) Invoke(ctx context.Context, s state.State, opts ...provider.InvokeOption) ([]artifact.Artifact, error) {
	messages := p.serializeMessages(s)

	var tools []provider.Tool
	var temperature float64
	for _, opt := range opts {
		if to, ok := opt.(toolOption); ok {
			tools = to.tools
		}
		if temp, ok := opt.(temperatureOption); ok {
			temperature = temp.t
		}
	}

	params := openai.ChatCompletionNewParams{
		Model:    openai.ChatModel(p.model),
		Messages: messages,
	}
	if len(tools) > 0 {
		params.Tools = p.serializeTools(tools)
	}
	if temperature != 0 {
		params.Temperature = param.NewOpt(temperature)
	}

	resp, err := p.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("chat completion: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	msg := resp.Choices[0].Message
	artifacts := make([]artifact.Artifact, 0)

	if len(msg.ToolCalls) > 0 {
		for _, tc := range msg.ToolCalls {
			artifacts = append(artifacts, artifact.ToolCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			})
		}
	} else {
		artifacts = append(artifacts, artifact.Text{Content: msg.Content})
	}

	if field, ok := msg.JSON.ExtraFields["reasoning_content"]; ok {
		var reasoning string
		if err := json.Unmarshal([]byte(field.Raw()), &reasoning); err == nil && reasoning != "" {
			artifacts = append(artifacts, artifact.Reasoning{Content: reasoning})
		}
	}

	if resp.Usage.PromptTokens > 0 || resp.Usage.CompletionTokens > 0 || resp.Usage.TotalTokens > 0 {
		artifacts = append(artifacts, artifact.Usage{
			PromptTokens:     int(resp.Usage.PromptTokens),
			CompletionTokens: int(resp.Usage.CompletionTokens),
			TotalTokens:      int(resp.Usage.TotalTokens),
		})
	}

	return artifacts, nil
}

// InvokeStreaming serializes state into an OpenAI streaming chat completions
// request, emits partial delta artifacts to deltasCh as they arrive, and
// returns the complete buffered artifacts when the stream finishes.
func (p *Provider) InvokeStreaming(ctx context.Context, s state.State, deltasCh chan<- artifact.Artifact, opts ...provider.InvokeOption) ([]artifact.Artifact, error) {
	if deltasCh == nil {
		return p.Invoke(ctx, s, opts...)
	}

	messages := p.serializeMessages(s)

	var tools []provider.Tool
	var temperature float64
	for _, opt := range opts {
		if to, ok := opt.(toolOption); ok {
			tools = to.tools
		}
		if temp, ok := opt.(temperatureOption); ok {
			temperature = temp.t
		}
	}

	params := openai.ChatCompletionNewParams{
		Model:    openai.ChatModel(p.model),
		Messages: messages,
	}
	if len(tools) > 0 {
		params.Tools = p.serializeTools(tools)
	}
	if temperature != 0 {
		params.Temperature = param.NewOpt(temperature)
	}

	stream := p.client.Chat.Completions.NewStreaming(ctx, params)

	var textContent strings.Builder
	var reasoningContent strings.Builder

	type toolCallAccum struct {
		id   string
		name strings.Builder
		args strings.Builder
	}
	toolCalls := make(map[int64]*toolCallAccum)

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

		if field, ok := delta.JSON.ExtraFields["reasoning_content"]; ok {
			var reasoning string
			if err := json.Unmarshal([]byte(field.Raw()), &reasoning); err == nil && reasoning != "" {
				reasoningContent.WriteString(reasoning)
				select {
				case deltasCh <- artifact.ReasoningDelta{Content: reasoning}:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
		}

		for _, tc := range delta.ToolCalls {
			acc, ok := toolCalls[tc.Index]
			if !ok {
				acc = &toolCallAccum{}
				toolCalls[tc.Index] = acc
			}
			if tc.ID != "" {
				acc.id = tc.ID
			}
			if tc.Function.Name != "" {
				acc.name.WriteString(tc.Function.Name)
			}
			if tc.Function.Arguments != "" {
				acc.args.WriteString(tc.Function.Arguments)
			}

			select {
			case deltasCh <- artifact.ToolCallDelta{
				ID:        acc.id,
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			}:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}

	if err := stream.Err(); err != nil {
		return nil, fmt.Errorf("streaming chat completion: %w", err)
	}

	var artifacts []artifact.Artifact

	if len(toolCalls) > 0 {
		indices := make([]int64, 0, len(toolCalls))
		for idx := range toolCalls {
			indices = append(indices, idx)
		}
		sort.Slice(indices, func(i, j int) bool { return indices[i] < indices[j] })
		for _, idx := range indices {
			acc := toolCalls[idx]
			artifacts = append(artifacts, artifact.ToolCall{
				ID:        acc.id,
				Name:      acc.name.String(),
				Arguments: acc.args.String(),
			})
		}
	} else if textContent.Len() > 0 {
		artifacts = append(artifacts, artifact.Text{Content: textContent.String()})
	}

	if reasoningContent.Len() > 0 {
		artifacts = append(artifacts, artifact.Reasoning{Content: reasoningContent.String()})
	}

	return artifacts, nil
}

// serializeTools converts provider-agnostic tool definitions into OpenAI SDK
// tool parameters.
func (p *Provider) serializeTools(tools []provider.Tool) []openai.ChatCompletionToolParam {
	toolParams := make([]openai.ChatCompletionToolParam, len(tools))
	for i, t := range tools {
		fnDef := openai.FunctionDefinitionParam{
			Name:       t.Name,
			Parameters: openai.FunctionParameters(t.Schema),
		}
		if t.Description != "" {
			fnDef.Description = param.NewOpt(t.Description)
		}
		toolParams[i] = openai.ChatCompletionToolParam{
			Function: fnDef,
		}
	}
	return toolParams
}
