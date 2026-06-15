package anthropic

import (
	"context"
	"fmt"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/state"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"go.opentelemetry.io/otel/trace"
)

// ThinkingConfig configures the reasoning capabilities of the model.
type ThinkingConfig struct {
	// Type specifies the reasoning mode. Valid values are "enabled" or "adaptive".
	Type string
	// BudgetTokens specifies the maximum number of tokens used for reasoning.
	BudgetTokens int
}

// Provider implements provider.Provider for the Anthropic Messages API.
type Provider struct {
	client  *anthropic.Client
	model   string
	tracer  trace.Tracer
	thinking ThinkingConfig
}

// config holds the build-time configuration for the Provider.
type config struct {
	apiKey           string
	model            string
	baseURL          string
	anthropicVersion string
	tracer           trace.Tracer
	thinking         *ThinkingConfig
}

// Option configures a Provider via the functional options pattern.
type Option func(*config)

// WithAPIKey sets the API key for the Anthropic provider.
func WithAPIKey(key string) Option {
	return func(c *config) {
		c.apiKey = key
	}
}

// WithModel sets the model identifier for the Anthropic provider.
func WithModel(model string) Option {
	return func(c *config) {
		c.model = model
	}
}

// WithBaseURL sets a custom API base URL (e.g., for OpenRouter).
func WithBaseURL(url string) Option {
	return func(c *config) {
		c.baseURL = url
	}
}

// WithAnthropicVersion sets the anthropic-version header. Defaults to "2023-06-01".
func WithAnthropicVersion(version string) Option {
	return func(c *config) {
		c.anthropicVersion = version
	}
}

// WithThinking configures the reasoning settings for the provider.
func WithThinking(cfg ThinkingConfig) Option {
	return func(c *config) {
		c.thinking = &cfg
	}
}

// WithTracer configures an OpenTelemetry tracer for the provider.
func WithTracer(tracer trace.Tracer) Option {
	return func(c *config) {
		c.tracer = tracer
	}
}

// New creates an Anthropic provider.
func New(opts ...Option) (*Provider, error) {
	cfg := &config{
		anthropicVersion: "2023-06-01",
	}
	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.apiKey == "" {
		return nil, fmt.Errorf("missing required option: apiKey")
	}
	if cfg.model == "" {
		return nil, fmt.Errorf("missing required option: model")
	}

	sdkOpts := []option.RequestOption{
		option.WithAPIKey(cfg.apiKey),
	}
	if cfg.baseURL != "" {
		sdkOpts = append(sdkOpts, option.WithBaseURL(cfg.baseURL))
	}
	if cfg.anthropicVersion != "" {
		sdkOpts = append(sdkOpts, option.WithHeader("anthropic-version", cfg.anthropicVersion))
	}

	client := anthropic.NewClient(sdkOpts...)

	thinking := ThinkingConfig{}
	if cfg.thinking != nil {
		thinking = *cfg.thinking
	}

	return &Provider{
		client:   &client,
		model:    cfg.model,
		tracer:   cfg.tracer,
		thinking: thinking,
	}, nil
}

// Invoke serializes state and calls the Anthropic Messages API.
func (p *Provider) Invoke(ctx context.Context, s state.State, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
	if p.tracer != nil {
		_, span := p.tracer.Start(ctx, "provider.invoke", trace.WithSpanKind(trace.SpanKindClient))
		defer span.End()
	}

	messages, systemPrompt := p.serializeMessages(s)

	params := anthropic.MessageNewParams{
		Model:    anthropic.Model(p.model),
		Messages: messages,
		System:   systemPrompt,
	}

	if p.thinking.Type != "" {
		params.Thinking = anthropic.ThinkingConfigParamOfEnabled(int64(p.thinking.BudgetTokens))
	}

	// Default max tokens if not provided
	params.MaxTokens = 4096

	stream := p.client.Messages.NewStreaming(ctx, params)

	for stream.Next() {
		event := stream.Current()
		switch e := event.AsAny().(type) {
		case anthropic.ContentBlockStartEvent:
			block := e.ContentBlock
			if block.Type == "tool_use" {
				select {
				case ch <- artifact.ToolCallDelta{
					ID:    block.ID,
					Name:  block.Name,
					Arguments: "", // Arguments follow in input_json_delta
				}:
				case <-ctx.Done():
					return ctx.Err()
				}
			}

		case anthropic.ContentBlockDeltaEvent:
			delta := e.Delta
			switch delta.Type {
			case "text_delta":
				select {
				case ch <- artifact.TextDelta{Content: delta.Text}:
				case <-ctx.Done():
					return ctx.Err()
				}
			case "thinking_delta":
				select {
				case ch <- artifact.ReasoningDelta{Content: delta.Thinking}:
				case <-ctx.Done():
					return ctx.Err()
				}
			case "input_json_delta":
				select {
				case ch <- artifact.ToolCallDelta{
					Arguments: delta.PartialJSON,
				}:
				case <-ctx.Done():
					return ctx.Err()
				}
			}

		case anthropic.MessageDeltaEvent:
			usage := e.Usage
			select {
			case ch <- artifact.Usage{
				PromptTokens:     int(usage.InputTokens),
				CompletionTokens: int(usage.OutputTokens),
				TotalTokens:      int(usage.InputTokens + usage.OutputTokens),
				CacheReadTokens:  int(usage.CacheReadInputTokens),
				CacheWriteTokens: int(usage.CacheCreationInputTokens),
			}:
			case <-ctx.Done():
				return ctx.Err()
			}
		case anthropic.MessageStopEvent:
			// Signal completion if needed (ore providers usually just return nil)
		}
	}

	if err := stream.Err(); err != nil {
		return fmt.Errorf("streaming completion: %w", err)
	}

	return nil
}

// Compile-time interface check.
var _ provider.Provider = (*Provider)(nil)

// serializeMessages converts ore state into Anthropic SDK message parameters.
// It maps ore roles to Anthropic roles and preserves Reasoning artifacts
// as thinking blocks to enable model continuity.
func (p *Provider) serializeMessages(s state.State) ([]anthropic.MessageParam, []anthropic.TextBlockParam) {
	turns := s.Turns()
	var systemPrompt []anthropic.TextBlockParam
	messages := make([]anthropic.MessageParam, 0, len(turns))

	for _, turn := range turns {
		var blocks []anthropic.ContentBlockParamUnion

		switch turn.Role {
		case state.RoleSystem:
			content := concatText(turn.Artifacts)
			systemPrompt = append(systemPrompt, anthropic.TextBlockParam{Text: content})
			continue

		case state.RoleUser:
			content := concatText(turn.Artifacts)
			blocks = append(blocks, anthropic.NewTextBlock(content))
			messages = append(messages, anthropic.MessageParam{
				Role:    "user",
				Content: blocks,
			})

		case state.RoleAssistant:
			for _, art := range turn.Artifacts {
				switch a := art.(type) {
				case artifact.Text:
					blocks = append(blocks, anthropic.NewTextBlock(a.Content))
				case artifact.Reasoning:
					blocks = append(blocks, anthropic.NewThinkingBlock("", a.Content))
				case artifact.ToolCall:
					blocks = append(blocks, anthropic.NewToolUseBlock(a.ID, a.Arguments, a.Name))
				}
			}
			messages = append(messages, anthropic.MessageParam{
				Role:    "assistant",
				Content: blocks,
			})

		case state.RoleTool:
			for _, art := range turn.Artifacts {
				if tr, ok := art.(artifact.ToolResult); ok {
					blocks = append(blocks, anthropic.NewToolResultBlock(tr.ToolCallID, tr.LLMString(), tr.IsError))
				}
			}
			if len(blocks) > 0 {
				messages = append(messages, anthropic.MessageParam{
					Role:    "user",
					Content: blocks,
				})
			}
		default:
			content := concatText(turn.Artifacts)
			blocks = append(blocks, anthropic.NewTextBlock(content))
			messages = append(messages, anthropic.MessageParam{
				Role:    "user",
				Content: blocks,
			})
		}
	}

	return messages, systemPrompt
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
