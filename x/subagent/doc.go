// Package subagent exposes an *agent.Agent as a tool.Tool, so a parent
// agent can invoke a sub-agent's inference through the same tool-use
// machinery as any other capability. The pattern of the wrapped agent
// is opaque to the parent: a SingleShot agent is a one-shot helper; a
// ReAct agent runs a tool-use loop; a Verified agent retries on quality
// failures. The parent sees only the final assistant turn.
//
// # Usage
//
//	sub := agent.New("researcher",
//	    agent.WithProvider(prov),
//	    agent.WithSpec(spec),
//	    agent.WithPattern(&cognitive.React),
//	)
//	subT, subFn := subagent.AsTool(sub, "researcher",
//	    "Search the codebase and answer the prompt.")
//	_ = registry.Register(subT, subFn)
//
// The sub-agent runs against a fresh ledger.Buffer seeded with the
// prompt as a RoleUser turn. The sub-agent's configured transforms,
// handlers, and pattern apply to that fresh buffer. State does not
// persist between sub-agent invocations.
//
// # Scope
//
// This package is a skeleton. The minimum surface is:
//
//   - AsTool: wraps an *agent.Agent as a tool.Tool, returning the
//     descriptor and callable ToolFunc.
//
// Streaming, parallel fan-out, and shared-state sub-agents are out of
// scope and tracked as follow-ups. See the plan tracked in #491 for
// the longer tail.
package subagent
