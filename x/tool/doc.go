// Package tool provides the loop.Handler bridge, concrete tool implementations,
// and tool discovery mechanisms for ore.
//
// Note: this is the x/tool extension package; it is typically imported as
// xtool (e.g. xtool "github.com/andrewhowdencom/ore/x/tool") to differentiate
// from the core tool package that defines the contracts.
//
// The core tool execution contracts (Registry interface, ToolFunc, RemoteSource,
// and ValidateSchema) live in the root tool/ package. This extension package
// bridges those contracts to the loop framework via Handler, and provides
// concrete tool implementations (bash, calculator, filesystem), MCP client
// integration, and skills discovery.
//
// A Handler implements loop.Handler. It detects artifact.ToolCall artifacts,
// looks up the tool by name in its registry (local or remote), executes the
// corresponding function, and emits a TurnCompleteEvent with RoleTool and a
// ToolResult artifact. Using Emit keeps tool results on the same observable
// event stream as other artifacts, allowing UI conduits to render them
// without special-casing. Unknown tools are refused with an error result.
//
// Tool calling composes three mechanisms:
//
//  1. Root tool/ package — provides the Registry interface, ToolFunc contract,
//     RemoteSource abstraction, and schema validation. This is the core
//     framework primitive.
//
//  2. Provider adapter (e.g., x/provider/openai/) — accepts tool configuration
//     per-invocation via openai.WithTools(), serializes them in requests,
//     deserializes ToolCall from responses, and serializes RoleTool turns
//     with ToolResult back to the provider.
//
//  3. Artifact Handler (this package) — bridges the root tool/ Registry to the
//     loop framework via NewHandler(), which implements loop.Handler.
//
// The application wires them together:
//
//	import (
//	    "github.com/andrewhowdencom/ore/tool"
//	    xtool "github.com/andrewhowdencom/ore/x/tool"
//	)
//
//	registry := tool.NewRegistry()
//	if err := registry.Register("add", "Add two numbers", schema, func(ctx context.Context, _ tool.Sandbox, args map[string]any) (any, error) {
//	    a, _ := args["a"].(float64)
//	    b, _ := args["b"].(float64)
//	    return a + b, nil
//	}); err != nil {
//	    ...
//	}
//
//	prov, err := openai.New(openai.WithAPIKey(apiKey), openai.WithModel(model))
//	if err != nil { ... }
//
//	// Registry.Tools() is the single source of truth.
//	step := loop.New(
//	    loop.WithHandlers(xtool.NewHandler(registry)),
//	    loop.WithInvokeOptions(openai.WithTools(registry.Tools())),
//	)
//
// MCP servers can be composed into the same registry via the root tool package:
//
//	mcpClient, err := mcp.NewClient(mcp.WithName("filesystem"), mcp.WithStdio("python", "server.py"))
//	if err != nil { ... }
//
//	registry := tool.NewRegistry(tool.WithMCPServer(mcpClient))
//
// Dynamic tool configuration. The tool list can be evolved during a session by
// passing different provider-level InvokeOption values (e.g. openai.WithTools or
// WithFilteredTools) to each Step.Turn call. This allows the application to
// prune, expand, or replace tools based on context, user permissions, or
// discovered capabilities:
//
//	// Pass different tool sets per-turn.
//	tools := selectToolsForContext(ctx, state)
//	_, err := step.Turn(ctx, state, prov, openai.WithTools(tools))
//
// The x/tool package also provides a convenience constructor for the common
// dynamic-filtering pattern. Instead of manually selecting tools per-turn,
// applications can register a filter function that is called automatically
// on each invocation:
//
//	filter := func(ctx context.Context, st state.State, tools []provider.Tool) []provider.Tool {
//	    // Return only tools permitted for the current user/role.
//	    return filterByRole(tools, st)
//	}
//	opt := WithFilteredTools(registry, filter)
//	_, err := step.Turn(ctx, state, prov, opt)
//
// A nil filter returns all tools from the registry unmodified.
//
// Because tools are passed per-invocation through provider.InvokeOption, there
// is no mutable provider state and no need for synchronization. The provider.Tool
// struct is provider-agnostic — each adapter maps it to its native API.
package tool
