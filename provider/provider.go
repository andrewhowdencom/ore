// Package provider defines the Provider interface, the contract between the core
// loop and concrete LLM provider adapters.
package provider

import (
	"context"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/state"
)

// InvokeOption is a marker interface for per-invocation configuration options.
// Concrete provider sub-packages define their own option types and exported
// constructors (e.g. openai.WithTools). Providers silently ignore options they
// do not recognize.
type InvokeOption interface {
	IsInvokeOption()
}

// Provider is the interface implemented by LLM provider adapters.
type Provider interface {
	// Invoke serializes the given state, calls the LLM API, and returns the
	// deserialized response artifacts.
	Invoke(ctx context.Context, s state.State, opts ...InvokeOption) ([]artifact.Artifact, error)
}

// StreamingProvider is the interface implemented by LLM provider adapters that
// support streaming response delivery. It composes Provider and adds the
// ability to emit ephemeral delta artifacts to a channel while the response is
// being generated. The adapter is responsible for buffering deltas internally
// and returning the complete artifacts once the stream finishes.
type StreamingProvider interface {
	Provider

	// InvokeStreaming serializes the given state, calls the LLM API with
	// streaming enabled, and emits partial delta artifacts to deltasCh as they
	// arrive. The channel is caller-provided and must not be closed by the
	// adapter. Once the stream completes, the adapter returns the full set of
	// buffered artifacts (analogous to Invoke). If deltasCh is nil, the
	// adapter may fall back to non-streaming behavior.
	InvokeStreaming(ctx context.Context, s state.State, deltasCh chan<- artifact.Artifact, opts ...InvokeOption) ([]artifact.Artifact, error)
}

// Tool describes a callable tool exposed to an LLM provider.
type Tool struct {
	Name        string
	Description string
	// Schema defines the JSON Schema for the tool's parameters.
	Schema map[string]any
}


