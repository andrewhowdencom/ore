// Package tool provides a provider-agnostic tool registry and artifact handler
// for ore.
//
// A Registry maps tool names to Go functions (ToolFunc). Each function receives
// parsed JSON arguments as a map[string]any and returns any result value,
// which is JSON-serialized before being sent back to the LLM.
//
// A Handler implements loop.Handler. It detects artifact.ToolCall artifacts,
// looks up the tool by name in its registry, executes the corresponding
// function, and appends a state.RoleTool turn with a artifact.ToolResult.
// Unknown tools are refused with an error result. This is deliberately an
// extension, not core behavior — it validates that the extension model works.
//
// Tool calling composes three mechanisms:
//
//   1. Provider adapter (e.g., provider/openai/) — accepts tool configuration
//      per-invocation via openai.WithTools(), serializes them in requests,
//      deserializes ToolCall from responses, and serializes RoleTool turns
//      with ToolResult back to the provider.
//   2. Artifact Handler (this package) — a Registry maps names to Go functions,
//      and a Handler implements loop.Handler to execute them.
//   3. Optional BeforeTurn hook (loop.BeforeTurn) — injects system prompts or
//      tool usage instructions before the provider call.
//
// The application wires them together:
//
//	registry := tool.NewRegistry()
//	registry.Register("add", func(ctx context.Context, args map[string]any) (any, error) {
//	    a, _ := args["a"].(float64)
//	    b, _ := args["b"].(float64)
//	    return a + b, nil
//	})
//
//	prov := openai.New(apiKey, model)
//
//	tools := []provider.Tool{
//	    {Name: "add", Description: "Add two numbers", Schema: schema},
//	}
//
//	// Pre-bind tool options to the Step so cognitive patterns remain provider-agnostic.
//	step := loop.New(
//	    loop.WithHandlers(registry.Handler()),
//	    loop.WithInvokeOptions(openai.WithTools(tools)),
//	)
//
// Dynamic tool configuration. The tool list can be evolved during a session by
// passing different openai.WithTools options to each Step.Turn call. This allows
// the application to prune, expand, or replace tools based on context, user
// permissions, or discovered capabilities:
//
//	// Pass different tool sets per-turn.
//	tools := selectToolsForContext(ctx, state)
//	_, err := step.Turn(ctx, state, prov, openai.WithTools(tools))
//
// Because tools are passed per-invocation through provider.InvokeOption, there
// is no mutable provider state and no need for synchronization. The provider.Tool
// struct is provider-agnostic — each adapter maps it to its native API.
package tool
