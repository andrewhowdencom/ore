package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	mcptypes "github.com/mark3labs/mcp-go/mcp"

	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/x/tool"
)

// Compile-time interface check.
var _ tool.RemoteSource = (*Client)(nil)

// caller is the subset of the mcp-go client used by Client.
// It is unexported to keep the public API clean while enabling mock-based
// testing of Call().
type caller interface {
	CallTool(ctx context.Context, request mcptypes.CallToolRequest) (*mcptypes.CallToolResult, error)
}

// Client implements tool.RemoteSource for an MCP server.
type Client struct {
	name   string
	caller caller
	tools  []provider.Tool
}

// NewClient creates an MCP client with the given options, performs the
// initialization handshake, and discovers available tools.
func NewClient(opts ...Option) (*Client, error) {
	cfg := &config{}
	for _, opt := range opts {
		opt(cfg)
	}

	var t transport.Interface
	var err error

	if cfg.command != "" {
		t = transport.NewStdio(cfg.command, nil, cfg.args...)
	} else if cfg.url != "" {
		sseOpts := buildSSEOptions(cfg)
		t, err = transport.NewSSE(cfg.url, sseOpts...)
		if err != nil {
			return nil, fmt.Errorf("create SSE transport: %w", err)
		}
	} else {
		return nil, fmt.Errorf("no transport configured: use WithStdio or WithSSE")
	}

	c := mcpclient.NewClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if cfg.command != "" {
		if err := c.Start(ctx); err != nil {
			return nil, fmt.Errorf("start stdio client: %w", err)
		}
	}

	initRequest := mcptypes.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcptypes.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcptypes.Implementation{
		Name:    "ore-mcp-client",
		Version: "0.1.0",
	}
	initRequest.Params.Capabilities = mcptypes.ClientCapabilities{}

	if _, err := c.Initialize(ctx, initRequest); err != nil {
		return nil, fmt.Errorf("initialize MCP client: %w", err)
	}

	toolsResult, err := c.ListTools(ctx, mcptypes.ListToolsRequest{})
	if err != nil {
		return nil, fmt.Errorf("list MCP tools: %w", err)
	}

	tools := make([]provider.Tool, len(toolsResult.Tools))
	for i, t := range toolsResult.Tools {
		schema, err := toolInputSchemaToMap(t.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("convert schema for tool %q: %w", t.Name, err)
		}
		tools[i] = provider.Tool{
			Name:        t.Name,
			Description: t.Description,
			Schema:      schema,
		}
	}

	return &Client{
		name:   cfg.name,
		caller: c,
		tools:  tools,
	}, nil
}

// Name returns the namespace prefix for this MCP source.
func (c *Client) Name() string { return c.name }

// Tools returns the list of tools available from the MCP server.
func (c *Client) Tools() []provider.Tool {
	t := make([]provider.Tool, len(c.tools))
	copy(t, c.tools)
	return t
}

// Call invokes a tool on the MCP server by name with the given arguments.
func (c *Client) Call(ctx context.Context, name string, args map[string]any) (any, error) {
	req := mcptypes.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args

	result, err := c.caller.CallTool(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("MCP tool call %q: %w", name, err)
	}

	if result.IsError {
		var texts []string
		for _, item := range result.Content {
			if text, ok := item.(mcptypes.TextContent); ok {
				texts = append(texts, text.Text)
			}
		}
		if len(texts) > 0 {
			return nil, fmt.Errorf("%s", texts[0])
		}
		return nil, fmt.Errorf("MCP tool call failed")
	}

	if result.StructuredContent != nil {
		return result.StructuredContent, nil
	}

	var texts []string
	for _, item := range result.Content {
		if text, ok := item.(mcptypes.TextContent); ok {
			texts = append(texts, text.Text)
		}
	}

	if len(texts) == 1 {
		return texts[0], nil
	}
	return texts, nil
}

// toolInputSchemaToMap converts an MCP ToolInputSchema to a
// map[string]any for use with provider.Tool.Schema.
func toolInputSchemaToMap(schema mcptypes.ToolInputSchema) (map[string]any, error) {
	data, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("marshal schema: %w", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("unmarshal schema: %w", err)
	}
	return m, nil
}

func buildSSEOptions(cfg *config) []transport.ClientOption {
	var opts []transport.ClientOption
	headers := make(map[string]string)
	for k, v := range cfg.headers {
		headers[k] = v
	}
	if cfg.bearerToken != "" {
		headers["Authorization"] = "Bearer " + cfg.bearerToken
	}
	if len(headers) > 0 {
		opts = append(opts, transport.WithHeaders(headers))
	}
	return opts
}

// Option configures a Client via the functional options pattern.
type Option func(*config)

type config struct {
	name        string
	command     string
	args        []string
	url         string
	bearerToken string
	headers     map[string]string
}

// WithName sets the namespace prefix for tools from this MCP source.
func WithName(name string) Option {
	return func(c *config) { c.name = name }
}

// WithStdio configures the client to communicate with an MCP server over stdio.
func WithStdio(command string, args ...string) Option {
	return func(c *config) {
		c.command = command
		c.args = args
	}
}

// WithSSE configures the client to communicate with an MCP server over SSE.
func WithSSE(url string) Option {
	return func(c *config) { c.url = url }
}

// WithBearerToken sets a Bearer token for SSE authentication.
func WithBearerToken(token string) Option {
	return func(c *config) { c.bearerToken = token }
}

// WithHeader sets a custom header for SSE transport.
func WithHeader(key, value string) Option {
	return func(c *config) {
		if c.headers == nil {
			c.headers = make(map[string]string)
		}
		c.headers[key] = value
	}
}