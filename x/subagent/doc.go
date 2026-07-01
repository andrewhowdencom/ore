// Package subagent implements a stack-frame sub-agent protocol: a
// parent agent hands off a free-text prompt to a freshly-constructed
// child *agent.Agent, and the child returns a structured Result
// validated against an enforced JSON schema. The frame is discarded
// after each invocation, giving strict state isolation between
// parent and child.
//
// # The protocol
//
// The parent calls a tool produced by AsTool. The tool's input is a
// single "prompt" string field. The closure:
//
//  1. Calls the application-supplied factory to obtain a fresh
//     *agent.Agent. The factory is the sole extension point: callers
//     compose the agent's pattern, model, and tool subset.
//  2. Seeds a fresh ledger.Buffer with the prompt and runs the
//     agent. The agent's bound state (if any) is NOT used — the
//     factory must omit agent.WithState to prevent the child from
//     mutating parent state.
//  3. Captures the produced turn from the agent's "turn_complete"
//     event stream (the same mechanism x/compaction uses).
//  4. Closes the agent. The fresh-per-call contract means a
//     factory that returns the same agent twice will fail on the
//     second call (the underlying step is closed).
//  5. Collects the turn's text, parses it against ResultSchema, and
//     returns a structured Result. A JSON parse error surfaces as
//     a tool error; a parseable-but-schema-invalid payload
//     (bad enum value, missing required field) surfaces as a
//     Result with Status set to StatusFailed so the parent can
//     branch on outcome without losing the diagnostic text.
//
// # Usage
//
//	sp, _ := subagent.ResultSystemPrompt()
//	factory := func() (*agent.Agent, error) {
//	    return agent.New("researcher",
//	        agent.WithProvider(prov),
//	        agent.WithSpec(spec),
//	        agent.WithPattern(&cognitive.React{}),
//	        agent.WithTransforms(sp),  // instruct the child to emit JSON
//	    ), nil
//	}
//	subT, subFn := subagent.AsTool(factory, "researcher",
//	    "Search the codebase and answer the prompt.")
//	_ = registry.Register(subT, subFn)
//
// # Structured return
//
// AsTool's closure returns a Result struct (or a tool error). The
// result is JSON-marshalled into the LLM-facing wire format with
// lowercase keys matching ResultSchema:
//
//	{"status": "success" | "partial" | "failed",
//	 "summary": string,
//	 "findings": object | null}
//
// Status values partition the outcome space:
//   - StatusSuccess: the sub-agent fully completed the task.
//   - StatusPartial: progress was made but the task was not finished.
//   - StatusFailed: the sub-agent could not accomplish the task, OR
//     the child's output failed schema validation (the raw payload
//     is preserved in Summary for diagnosis).
//
// The parent can branch on outcome directly:
//
//	res, err := fn(ctx, sandbox, args)
//	switch r := res.(type) {
//	case subagent.Result:
//	    switch r.Status {
//	    case subagent.StatusSuccess:
//	        // done
//	    case subagent.StatusPartial:
//	        // retry or escalate
//	    case subagent.StatusFailed:
//	        // log r.Summary, retry or escalate
//	    }
//	case nil:
//	    // err is set; tool failed (parse error, agent error, etc.)
//	}
//
// # System prompt helper
//
// ResultSystemPrompt returns a loop.Transform preconfigured with the
// schema baked into the system prompt. Factories should include this
// in the child agent's WithTransforms so the child is instructed to
// emit JSON matching ResultSchema. AsTool cannot inject the
// transform post-construction because agent.Agent does not expose
// transform mutation; the factory's responsibility is to include
// it at build time.
//
// # Scope
//
// Out of scope for v1: streaming deltas, parallel fan-out, span
// nesting under the parent, dynamic tool-set changes within a
// single call. See issue #517 for the design rationale.
package subagent
