// Package systemprompt provides a loop.Transform that injects a system
// prompt into the inference context without mutating the persistent
// conversation buffer.
//
// The transform prepends a single state.RoleSystem turn containing an
// artifact.Text artifact whose content is evaluated lazily on each
// Transform call. This enables dynamic system prompts that can change
// between turns — for example, by reading from thread metadata that a
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
// Dynamic system prompt that reads from thread metadata:
//
//	// Assuming `thr` is a *session.Thread captured in outer scope.
//	transform, err := systemprompt.New(systemprompt.WithContentFunc(func() string {
//		if p, ok := thr.GetMetadata("persona"); ok {
//			return "You are a " + p + "."
//		}
//		return "You are a helpful assistant."
//	}))
//	if err != nil {
//		panic(err)
//	}
//	step := loop.New(loop.WithTransforms(transform))
//
// Content functions are re-evaluated on every Transform call, so
// applications can close over mutable state (e.g., thread.Metadata) to
// switch personas or roles dynamically.
package systemprompt
