// Package guardrails provides a loop.Transform that injects safety and
// formatting constraints into the inference context without mutating the
// persistent conversation buffer.
//
// Each configured rule is injected as a separate state.RoleUser turn
// containing a artifact.Text artifact. Using RoleUser (rather than
// RoleSystem) gives the guardrails the weight of user instructions,
// distinct from the persona set by x/systemprompt.
//
// # Forge Blueprint Usage
//
//	transforms:
//	  - module: github.com/andrewhowdencom/ore/x/guardrails
//	    options:
//	      rules:
//	        - "Never execute rm -rf /"
//	        - "Always format code in markdown blocks"
//
// # Hand-Compiled Usage
//
//	import "github.com/andrewhowdencom/ore/x/guardrails"
//
//	transform := guardrails.New(
//	    guardrails.WithRules(
//	        "Never execute rm -rf /",
//	        "Always format code in markdown blocks",
//	    ),
//	)
//	step := loop.New(loop.WithTransforms(transform))
package guardrails
