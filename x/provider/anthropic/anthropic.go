// Package anthropic provides the first-party Anthropic provider adapter.
//
// It is the recommended import path for application code that wants to
// call the Anthropic Messages API. The package composes the wire
// implementation at github.com/andrewhowdencom/ore/x/wire/anthropic and
// applies first-party defaults (currently identity — canonical spec
// names are forwarded verbatim to the upstream API).
//
// Application code imports only this package; the wire is a transitive
// dependency. Each wire option is re-exported below as a function on
// this package so callers can write `anthropic.WithAPIKey(...)` without
// importing the wire directly. Future versions may add vendor-specific
// defaults (catalog, base URL, identity resolver) here without breaking
// call sites — the first-party wrapper exists precisely so that a single
// package can absorb such changes.
package anthropic

import (
	"net/http"

	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/tool"
	anthropicwire "github.com/andrewhowdencom/ore/x/wire/anthropic"

	"go.opentelemetry.io/otel/trace"
)

// Option configures a first-party Anthropic provider. It is an alias of
// [anthropicwire.Option] so callers can write `anthropic.WithAPIKey(...)`
// or import the wire's options directly — both forms are accepted by
// [New].
type Option = anthropicwire.Option

// New constructs a first-party Anthropic provider. The first-party
// wrapper currently composes the wire implementation with identity
// resolution (canonical spec names forwarded verbatim). Vendor-specific
// defaults — catalog lookup, a default base URL, a custom name
// resolver — are intended to live here in future versions without
// breaking call sites.
//
// The returned value implements [provider.Provider] but is the wire's
// concrete *Provider type; callers should depend on the interface.
func New(opts ...Option) (provider.Provider, error) {
	return anthropicwire.New(opts...)
}

// ---------------------------------------------------------------------------
// Option re-exports. Each wire option is wrapped in a package-level
// function on the first-party package so callers do not need to import
// the wire package directly. The wrappers are zero-cost (a single
// forward call); they exist only to give callers a single import path.
// ---------------------------------------------------------------------------

// WithAPIKey sets the API key for the Anthropic provider.
func WithAPIKey(key string) Option { return anthropicwire.WithAPIKey(key) }

// WithBaseURL sets a custom API base URL.
func WithBaseURL(url string) Option { return anthropicwire.WithBaseURL(url) }

// WithHTTPClient sets a custom HTTP client for the provider.
func WithHTTPClient(client *http.Client) Option { return anthropicwire.WithHTTPClient(client) }

// WithNameResolver sets a function that translates the canonical
// [models.Spec.Name] into the wire name understood by the upstream host.
func WithNameResolver(r func(canonical string) string) Option {
	return anthropicwire.WithNameResolver(r)
}

// WithTracer configures an OpenTelemetry tracer for the provider.
func WithTracer(tracer trace.Tracer) Option { return anthropicwire.WithTracer(tracer) }

// ---------------------------------------------------------------------------
// InvokeOption re-exports. These are per-call options consumed via
// [loop.WithInvokeOptions]; they configure individual requests rather
// than the provider itself.
// ---------------------------------------------------------------------------

// WithTools configures the set of available tools for the request.
func WithTools(tools []tool.Tool) provider.InvokeOption { return anthropicwire.WithTools(tools) }

// WithTemperature sets the sampling temperature for the request.
func WithTemperature(t float64) provider.InvokeOption { return anthropicwire.WithTemperature(t) }

// WithMaxTokens sets the maximum number of tokens to generate.
func WithMaxTokens(n int64) provider.InvokeOption { return anthropicwire.WithMaxTokens(n) }

// WithThinkingLevel sets the model's reasoning effort level.
func WithThinkingLevel(l models.ThinkingLevel) provider.InvokeOption {
	return anthropicwire.WithThinkingLevel(l)
}
