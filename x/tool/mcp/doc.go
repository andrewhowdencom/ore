// Package mcp provides an MCP (Model Context Protocol) client that implements
// the tool.RemoteSource interface, allowing ore tool.Registry to compose
// remote tools discovered from MCP servers alongside local Go functions.
//
// The client supports stdio and SSE transports. For SSE, authentication is
// configured via Bearer token or custom headers. Tools are discovered during
// client initialization and cached; the namespace prefix is applied by the
// Registry when merging local and remote tools.
//
// Stdio usage:
//
//	client, err := mcp.NewClient(
//	    mcp.WithName("filesystem"),
//	    mcp.WithStdio("python", "mcp_server.py"),
//	)
//	if err != nil { ... }
//
//	registry := tool.NewRegistry(tool.WithMCPServer(client))
//
// SSE usage with Bearer token:
//
//	client, err := mcp.NewClient(
//	    mcp.WithName("search"),
//	    mcp.WithSSE("https://mcp.example.com/sse"),
//	    mcp.WithBearerToken("token"),
//	)
//	if err != nil { ... }
//
//	registry := tool.NewRegistry(tool.WithMCPServer(client))
package mcp