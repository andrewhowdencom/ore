// Package provider defines the Provider interface, the contract between the core
// loop and concrete LLM provider adapters.
package provider

import (
	"context"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/state"
	"github.com/andrewhowdencom/ore/tool"
)

// InvokeOption is a marker interface for per-invocation configuration options.
// Concrete provider sub-packages define their own option types and exported
// constructors (e.g. openai.WithTools). Providers silently ignore options they
// do not recognize.
type InvokeOption interface {
	IsInvokeOption()
}

// ToolsOption is a per-invocation option that configures available tools.
// The Tools field is a function so the tool list can be resolved dynamically
// based on the current context and conversation state (e.g. filtering by
// permissions, user role, or runtime discovery).
type ToolsOption struct {
	// Tools returns the slice of tools available for the current invocation.
	// The function receives the request context and the current state, enabling
	// per-turn dynamic selection of tools. Returning nil is equivalent to an
	// empty tool list (no tools are offered to the provider).
	Tools func(ctx context.Context, st state.State) []tool.Tool
}

// IsInvokeOption marks ToolsOption as a provider.InvokeOption.
func (ToolsOption) IsInvokeOption() {}

// WithTools returns an InvokeOption that configures the set of available tools
// for a single provider invocation. It is a convenience wrapper for the common
// case where the tool set is static; it creates a ToolsOption whose Tools
// function simply returns the provided slice, ignoring the request context and
// state.
func WithTools(tools []tool.Tool) InvokeOption {
	return ToolsOption{Tools: func(context.Context, state.State) []tool.Tool { return tools }}
}

// MaxTokensOption is a per-invocation option that sets the maximum
// number of tokens the model is permitted to generate on a single
// invocation. It is the provider-agnostic counterpart to
// per-adapter helpers (e.g. anthropic.WithMaxTokens,
// openai.WithMaxTokens) so that callers in framework code paths
// (e.g. compaction strategies, slash commands) can request a
// specific budget without importing a concrete adapter package.
//
// Adapters must translate the value into their provider's wire
// format and must treat N <= 0 as a no-op (omit the field). A
// zero or negative N is "the caller has no opinion; use whatever
// default the adapter / model provides."
type MaxTokensOption struct {
	// N is the maximum number of tokens. N <= 0 means "no
	// opinion" — the adapter should omit the field on the wire
	// and let its default apply. Adapters must NOT default N
	// to a small sentinel value (e.g. 1) to "fail loudly":
	// such a value produces silent garbage, not a loud failure.
	N int64
}

// IsInvokeOption marks MaxTokensOption as a provider.InvokeOption.
func (MaxTokensOption) IsInvokeOption() {}

// WithMaxTokens returns an InvokeOption that sets the maximum
// number of tokens the model is permitted to generate on a single
// invocation. It is the provider-agnostic counterpart to
// per-adapter helpers; adapters translate the value into their
// own wire format at request time.
//
// Pass a value <= 0 to indicate "no opinion" — the adapter will
// omit the field and use its own default. Callers that need a
// specific budget (e.g. compaction strategies producing a long
// summary) should pass an explicit value appropriate to the
// model and the task.
func WithMaxTokens(n int64) InvokeOption {
	return MaxTokensOption{N: n}
}

// Provider is the interface implemented by LLM provider adapters.
type Provider interface {
	// Invoke serializes the given state, calls the LLM API, and emits
	// deserialized response artifacts to the provided channel.
	//
	// The adapter must emit each artifact as soon as the native API delivers a
	// chunk, preserving that arrival order.
	//
	// Adapters must emit canonical ore artifact types as soon as the native
	// API delivers a chunk, preserving that arrival order. Fragmented data
	// (e.g. streaming text or tool-call chunks) is emitted as delta types
	// (TextDelta, ToolCallDelta, etc.) and accumulated by the core loop.
	// Adapters should not perform their own accumulation except when the
	// native format genuinely cannot be expressed as an ore artifact type.
	//
	// The spec carries the model identity and inference configuration.
	// Adapters translate the spec to their provider's wire format. A zero-
	// value spec (empty Name, zero values for all fields) is valid: the
	// adapter falls back to its constructor defaults and omits fields where
	// the framework / model has a default.
	//
	// The channel must not be closed by the adapter.
	Invoke(ctx context.Context, s state.State, spec models.Spec, ch chan<- artifact.Artifact, opts ...InvokeOption) error
}
