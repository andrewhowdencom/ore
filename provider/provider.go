// Package provider defines the Provider interface, the contract between the core
// loop and concrete LLM provider adapters.
package provider

import (
	"context"
	"fmt"

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

// ThinkingLevel is a portable, qualitative description of how much
// reasoning effort a model should spend on a turn. Adapters translate
// the level into their provider's wire format at request time.
//
// Levels are case-sensitive lowercase strings. The level is the user's
// intent; the adapter is the translator. The empty string is not a
// valid level — callers should substitute their own default (commonly
// ThinkingLevelOff) before calling ParseThinkingLevel.
type ThinkingLevel string

const (
	// ThinkingLevelOff disables extended thinking. Adapters must not
	// send a `thinking` field (or equivalent) when this level is
	// requested; the request is identical to a non-thinking request.
	ThinkingLevelOff ThinkingLevel = "off"

	// ThinkingLevelMinimal asks for the smallest amount of thinking
	// the provider supports. Useful as a low-cost pipeline probe.
	ThinkingLevelMinimal ThinkingLevel = "minimal"

	// ThinkingLevelLow asks for a small amount of thinking.
	ThinkingLevelLow ThinkingLevel = "low"

	// ThinkingLevelMedium asks for a moderate amount of thinking.
	// Recommended default for reasoning-capable models when the
	// application or user opts in.
	ThinkingLevelMedium ThinkingLevel = "medium"

	// ThinkingLevelHigh asks for a substantial amount of thinking.
	ThinkingLevelHigh ThinkingLevel = "high"

	// ThinkingLevelMax asks for the maximum amount of thinking the
	// provider allows, while still leaving room for the visible
	// response. Adapters may clamp this to their maximum.
	ThinkingLevelMax ThinkingLevel = "max"
)

// Valid reports whether the level is one of the defined constants.
// The empty string is not valid.
func (l ThinkingLevel) Valid() bool {
	switch l {
	case ThinkingLevelOff, ThinkingLevelMinimal, ThinkingLevelLow,
		ThinkingLevelMedium, ThinkingLevelHigh, ThinkingLevelMax:
		return true
	}
	return false
}

// ParseThinkingLevel parses a string into a ThinkingLevel. The empty
// string is treated as a parse error — callers should substitute their
// own default (commonly ThinkingLevelOff) before calling. Levels are
// case-sensitive lowercase.
func ParseThinkingLevel(s string) (ThinkingLevel, error) {
	l := ThinkingLevel(s)
	if !l.Valid() {
		return "", fmt.Errorf("invalid thinking level %q: must be one of off, minimal, low, medium, high, max", s)
	}
	return l, nil
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



