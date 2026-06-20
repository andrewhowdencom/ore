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
// # Hot-Swappable Content Sources
//
// When a content function's underlying state may change after the
// Transform has been constructed — for example, a file path that should
// reflect "the currently active identity" — implement the Resolver
// interface and register it once:
//
//	import (
//	    "github.com/andrewhowdencom/ore/x/systemprompt"
//	    "github.com/andrewhowdencom/ore/x/systemprompt/source"
//	)
//
//	active := source.NewFileResolver(initialPath)
//	sp, _ := systemprompt.New(
//	    systemprompt.WithContentFunc(active.Resolve),
//	)
//
// To swap the active content later, mutate the resolver directly:
//
//	active.SetPath(newPath)
//
// The next Transform call reads from newPath. No new WithContentFunc
// call is required, which avoids the "stacking" pattern that produces
// contradictory content when multiple agent definitions are
// concatenated into a single RoleSystem turn.
package systemprompt
