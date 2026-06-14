// Package provider defines the Provider interface, the contract between the core
// loop and concrete LLM provider adapters.
package provider

import (
	"context"

	"github.com/andrewhowdencom/ore/artifact"
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

// ModelOption is a per-invocation option that overrides the model name used
// for a single provider invocation. The Model field is a string so callers
// can source it from arbitrary input (e.g. session metadata).
//
// An empty Model field is treated as a no-op by adapters; it does not clear
// the model or fall back to a default. Adapters that honor ModelOption must
// continue to use the value supplied at construction when Model is empty.
// This preserves the precedence rule: per-invocation option > constructor.
type ModelOption struct {
	// Model is the model name to use for the current invocation. Adapters
	// must treat an empty string as a no-op.
	Model string
}

// IsInvokeOption marks ModelOption as a provider.InvokeOption.
func (ModelOption) IsInvokeOption() {}

// WithModel returns an InvokeOption that overrides the model name for a
// single provider invocation. Passing an empty string is a no-op: adapters
// must keep using the value supplied at construction.
//
// Note: the per-adapter constructor option (e.g. openai.WithModel) shares
// the same bare name by design. Call sites should disambiguate with the
// package qualifier, e.g. provider.WithModel(...) at the call to Invoke.
func WithModel(name string) InvokeOption {
	return ModelOption{Model: name}
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
	// The channel must not be closed by the adapter.
	Invoke(ctx context.Context, s state.State, ch chan<- artifact.Artifact, opts ...InvokeOption) error
}



