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

// Compile-time interface check.
var _ provider.Provider = (*Provider)(nil)

// Invoke serializes state and calls the Anthropic Messages API.
func (p *Provider) Invoke(ctx context.Context, s state.State, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
	// TODO: Implement streaming logic in Task 3
	return fmt.Errorf("not implemented")
}
