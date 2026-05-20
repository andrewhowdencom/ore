// Package mcp provides an MCP (Model Context Protocol) client that implements
// the tool.RemoteSource interface, allowing ore tool.Registry to compose
// remote tools discovered from MCP servers alongside local Go functions.
//
// The client supports stdio and SSE transports, with authentication options
// for SSE (Bearer token, custom headers). Tools are discovered during client
// initialization and cached; the namespace prefix is applied by the Registry.
//
// Usage:
//
//	client, err := mcp.NewClient(
//	    mcp.WithName("filesystem"),
//	    mcp.WithStdio("python", "mcp_server.py"),
//	)
//	if err != nil { ... }
//
//	registry := tool.NewRegistry(tool.WithMCPServer(client))
package mcp