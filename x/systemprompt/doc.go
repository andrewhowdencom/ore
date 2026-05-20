// Package systemprompt provides a loop.Transform that injects a static
// system prompt into the inference context without mutating the persistent
// conversation buffer.
//
// The transform prepends a single state.RoleSystem turn containing a
// artifact.Text artifact with the configured content. It uses
// state.NewVirtualTurnState for zero-copy injection, so the underlying
// buffer is never modified.
//
// # Forge Blueprint Usage
//
//	transforms:
//	  - module: github.com/andrewhowdencom/ore/x/systemprompt
//	    options:
//	      content: "You are a helpful assistant."
//
// # Hand-Compiled Usage
//
//	import "github.com/andrewhowdencom/ore/x/systemprompt"
//
//	transform := systemprompt.New(systemprompt.WithContent("You are a helpful assistant."))
//	step := loop.New(loop.WithTransforms(transform))
//
// The content is static per-transform instance. Dynamic system prompts
// are achieved by creating a new transform instance or using a future
// dynamic transform module.
package systemprompt
