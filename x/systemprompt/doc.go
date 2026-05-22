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
// # Usage
//
//	import "github.com/andrewhowdencom/ore/x/systemprompt"
//
// Basic static system prompt:
//
//	transform := systemprompt.New(systemprompt.WithContentFunc(func() string {
//		return "You are a helpful assistant."
//	}))
//	step := loop.New(loop.WithTransforms(transform))
//
// Dynamic system prompt that reads from thread metadata:
//
//	// Assuming `thr` is a *thread.Thread captured in outer scope.
//	transform := systemprompt.New(systemprompt.WithContentFunc(func() string {
//		if p, ok := thr.GetMetadata("persona"); ok {
//			return "You are a " + p + "."
//		}
//		return "You are a helpful assistant."
//	}))
//	step := loop.New(loop.WithTransforms(transform))
//
// The content function is re-evaluated on every Transform call, so
// applications can close over mutable state (e.g., thread.Metadata) to
// switch personas or roles dynamically.
package systemprompt
