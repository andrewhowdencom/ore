// Package anthropic implements a provider adapter for the Anthropic Messages
// API and the OpenRouter /api/v1/messages mirror. It wraps the official
// github.com/anthropics/anthropic-sdk-go client.
package anthropic

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/state"
	"github.com/andrewhowdencom/ore/tool"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"go.opentelemetry.io/otel/trace"
)

// Provider implements provider.Provider for the Anthropic Messages API
// (Anthropic native and the OpenRouter /api/v1/messages mirror) using
// the official anthropic-sdk-go SDK.
//
// Like the openai module, the provider resolves host identity once at
// construction time so the read- and write-sides agree on auth header
// selection, base URL, and SDK options without re-walking the option
// list on every Invoke.
type Provider struct {
	client anthropic.Client
	model  string
	tracer trace.Tracer
	// isOpenRouter is the resolved boolean used to choose the
	// OpenRouter branch (`Authorization: Bearer <key>`) over the
	// Anthropic native branch (`x-api-key: <key>`). Resolved at
	// construction from the configured base URL so the auth header
	// cannot drift between turns of the same Provider.
	isOpenRouter bool
}

// WithTools returns an InvokeOption that configures the set of available tools
// for a single provider invocation. It delegates to the provider-agnostic
// provider.WithTools for cross-adapter compatibility.
func WithTools(tools []tool.Tool) provider.InvokeOption {
	return provider.WithTools(tools)
}

// config holds the build-time configuration for the Provider.
type config struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient option.HTTPClient
	tracer     trace.Tracer
}

// Option configures a Provider via the functional options pattern.
type Option func(*config)

// WithAPIKey sets the API key for the Anthropic-compatible provider. The key
// is interpreted as an Anthropic native key when the configured base URL is
// empty or targets api.anthropic.com; it is interpreted as an OpenRouter key
// when the configured base URL targets openrouter.ai.
func WithAPIKey(key string) Option {
	return func(c *config) {
		c.apiKey = key
	}
}

// WithModel sets the model identifier for the Anthropic-compatible provider.
func WithModel(model string) Option {
	return func(c *config) {
		c.model = model
	}
}

// WithBaseURL sets a custom API base URL. Use this to point at the OpenRouter
// /api/v1/messages mirror, a local proxy, or any other Anthropic-compatible
// host. The provider inspects the base URL to choose the auth header
// (`x-api-key` on Anthropic native, `Authorization: Bearer <key>` on
// OpenRouter).
func WithBaseURL(url string) Option {
	return func(c *config) {
		c.baseURL = url
	}
}

// WithHTTPClient sets a custom HTTP client for the provider. This is primarily
// useful for testing; the SDK's default client is appropriate for production.
func WithHTTPClient(client *http.Client) Option {
	return func(c *config) {
		if client == nil {
			return
		}
		c.httpClient = client
	}
}

// WithTracer configures an OpenTelemetry tracer for the provider. When set,
// Invoke opens a `provider.invoke` span with `SpanKindClient` and records
// the `model` and `thread_id` attributes. When unset, Invoke is a
// no-op for tracing.
func WithTracer(tracer trace.Tracer) Option {
	return func(c *config) {
		c.tracer = tracer
	}
}

// New creates an Anthropic-compatible provider. WithAPIKey and WithModel are
// required; all other options are optional. The base URL is inspected once at
// construction time to decide which auth header to apply, so callers do not
// need to track host identity across invocations.
func New(opts ...Option) (*Provider, error) {
	cfg := &config{}
	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.apiKey == "" {
		return nil, fmt.Errorf("missing required option: apiKey")
	}
	if cfg.model == "" {
		return nil, fmt.Errorf("missing required option: model")
	}

	sdkOpts := buildSDKOptions(cfg)

	return &Provider{
		client:      anthropic.NewClient(sdkOpts...),
		model:       cfg.model,
		tracer:      cfg.tracer,
		isOpenRouter: isOpenRouter(cfg.baseURL),
	}, nil
}

// Compile-time interface check.
var _ provider.Provider = (*Provider)(nil)

// Invoke is the streaming entry point. The skeleton in this task returns
// nil unconditionally; full implementation lands in later tasks. The stub
// signature matches provider.Provider so the compile-time interface check
// succeeds and downstream callers can wire the provider up before
// streaming is implemented.
func (p *Provider) Invoke(ctx context.Context, s state.State, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
	_ = ctx
	_ = s
	_ = ch
	_ = opts
	return nil
}

// isOpenRouter reports whether the configured base URL targets OpenRouter.
// Detection is a substring match; OpenRouter publishes only that domain
// (and its subdomains), so a stricter URL parser would add complexity
// without meaningfully reducing false positives. Mirrors the openai
// module's `isOpenRouter` heuristic.
func isOpenRouter(baseURL string) bool {
	return strings.Contains(baseURL, "openrouter.ai")
}

// buildSDKOptions assembles the SDK option list at construction time. The
// auth header is selected here (not per invocation) so it cannot drift
// between turns of the same Provider. The skeleton applies the
// Anthropic-native default (`x-api-key`); the full host-aware dispatch
// lands in Task 8 alongside the OpenRouter test.
func buildSDKOptions(cfg *config) []option.RequestOption {
	sdkOpts := []option.RequestOption{option.WithAPIKey(cfg.apiKey)}
	if cfg.baseURL != "" {
		sdkOpts = append(sdkOpts, option.WithBaseURL(cfg.baseURL))
	}
	if cfg.httpClient != nil {
		sdkOpts = append(sdkOpts, option.WithHTTPClient(cfg.httpClient))
	}
	return sdkOpts
}
