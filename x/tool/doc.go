// Package tool provides a provider-agnostic tool registry and artifact handler
// for ore.
//
// A Registry maps tool names to Go functions (ToolFunc) together with their
// metadata (description and JSON schema). Each function receives parsed JSON
// arguments as a map[string]any and returns any result value, which is
// JSON-serialized before being sent back to the LLM.
//
// The Registry can also compose remote tools discovered from MCP servers via
// the RemoteSource interface, making it a single source of truth for tool
// metadata and execution. Remote tools are namespaced under their source
// prefix (e.g., "filesystem/read_file").
//
// A Handler implements loop.Handler. It detects artifact.ToolCall artifacts,
// looks up the tool by name in its registry (local or remote), executes the
// corresponding function, and appends a state.RoleTool turn with a
// artifact.ToolResult. Unknown tools are refused with an error result. This is
// deliberately an extension, not core behavior — it validates that the
// extension model works.
//
// Tool calling composes two mechanisms:
//
//   1. Provider adapter (e.g., provider/openai/) — accepts tool configuration
//      per-invocation via openai.WithTools(), serializes them in requests,
//      deserializes ToolCall from responses, and serializes RoleTool turns
//      with ToolResult back to the provider.
//   2. Artifact Handler (this package) — a Registry maps names to Go functions
//      and remote sources, and a Handler implements loop.Handler to execute
//      them.
//
// The application wires them together:
//
//	registry := tool.NewRegistry()
//	registry.Register("add", "Add two numbers", schema, func(ctx context.Context, args map[string]any) (any, error) {
//	    a, _ := args["a"].(float64)
//	    b, _ := args["b"].(float64)
//	    return a + b, nil
//	})
//
//	prov := openai.New(apiKey, model)
//
//	// Registry.Tools() is now the single source of truth.
//	step := loop.New(
//	    loop.WithHandlers(registry.Handler()),
//	    loop.WithInvokeOptions(openai.WithTools(registry.Tools())),
//	)
//
// MCP servers can be composed into the same registry:
//
//	mcpClient, err := mcp.NewClient(mcp.WithName("filesystem"), mcp.WithStdio("python", "server.py"))
//	if err != nil { ... }
//
//	registry := tool.NewRegistry(tool.WithMCPServer(mcpClient))
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