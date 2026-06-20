// Package systemprompt provides a loop.Transform that injects a system
// prompt into the inference context without mutating the persistent
// conversation buffer.
//
// The transform prepends a single state.RoleSystem turn containing an
// artifact.Text artifact whose content is evaluated lazily on each
// Transform call. This enables dynamic system prompts that can change
// between turns — for example, by reading from stream metadata that a
// tool call has updated mid-session.
//
// Multiple content functions can be composed via WithContentFunc or
// WithContentFuncs. Fragments are evaluated in registration order,
// empty results are omitted, and non-empty results are concatenated
// with "\n\n" separators into a single RoleSystem turn.
//
// # Usage
//
//	import "github.com/andrewhowdencom/ore/x/systemprompt"
//
// Basic static system prompt:
//
//	transform, err := systemprompt.New(systemprompt.WithContentFunc(func() string {
//		return "You are a helpful assistant."
//	}))
//	if err != nil {
//		panic(err)
//	}
//	step := loop.New(loop.WithTransforms(transform))
//
// Composable prompt fragments from multiple sources:
//
//	fragmentA := func() string { return "Follow the style guide." }
//	fragmentB := func() string { return "Use markdown for code." }
//	transform, err := systemprompt.New(
//		systemprompt.WithContentFunc(func() string {
//			return "You are a helpful assistant."
//		}),
//		systemprompt.WithContentFuncs(fragmentA, fragmentB),
//	)
//	if err != nil {
//		panic(err)
//	}
//	step := loop.New(loop.WithTransforms(transform))
//
// Dynamic system prompt that reads from stream metadata:
//
//	// Assuming `stream` is a *session.Stream captured in the stepFactory closure.
//	sp, err := systemprompt.New(systemprompt.WithContentFunc(func() string {
//		if p, ok := stream.GetMetadata("persona"); ok {
//			return "You are a " + p + "."
//		}
//		return "You are a helpful assistant."
//	}))
//	if err != nil {
//		panic(err)
//	}
//	return []loop.Option{loop.WithTransforms(sp)}, nil
//
// Content functions are re-evaluated on every Transform call, so
// applications can close over mutable state (e.g., stream.Metadata) to
// switch personas or roles dynamically.
//
// # Multi-Identity Stacking
//
// The Transform has no opinion about what each fragment represents. When
// multiple fragments each carry a complete agent definition (a body that
// starts with "## Identity" and prescribes operational rules), the
// resulting RoleSystem turn contains multiple "## Identity" sections with
// mutually exclusive instructions. The LLM then has no internal mechanism
// to determine which identity is active and may refuse valid actions or
// perform actions it shouldn't.
//
// This is a consumer composition error, not a transform bug. To avoid
// it, load agent definitions via the dedicated helpers in
// x/systemprompt/source: source.Agent loads exactly one active identity
// from "<dir>/<name>.md", and source.AgentReferenceIndex renders the
// other available agents as a compact bullet list. Pair the two so the
// active agent's full body is in context, and the rest are summarised
// by name + description without contradicting operational rules:
//
//	import (
//	    "github.com/andrewhowdencom/ore/x/systemprompt"
//	    "github.com/andrewhowdencom/ore/x/systemprompt/source"
//	)
//
//	transform, _ := systemprompt.New(
//	    systemprompt.WithContentFuncs(
//	        source.Agent("/path/to/agents", activeName),
//	        source.AgentReferenceIndex("/path/to/agents", activeName),
//	    ),
//	)
//
// See x/systemprompt/source for the full agent-loading API.
package systemprompt
