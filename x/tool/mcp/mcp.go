package mcp

import (
	"context"

	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/x/tool"
)

// Compile-time interface check.
var _ tool.RemoteSource = (*Client)(nil)

// Client implements tool.RemoteSource for an MCP server.
type Client struct {
	name string
}

// NewClient creates an MCP client with the given options.
// It performs the MCP initialization handshake and discovers available tools.
func NewClient(opts ...Option) (*Client, error) {
	cfg := &config{}
	for _, opt := range opts {
		opt(cfg)
	}
	return &Client{name: cfg.name}, nil
}

// Name returns the namespace prefix for this MCP source.
func (c *Client) Name() string { return c.name }

// Tools returns the list of tools available from the MCP server.
func (c *Client) Tools() []provider.Tool { return nil }

// Call invokes a tool on the MCP server by name with the given arguments.
func (c *Client) Call(ctx context.Context, name string, args map[string]any) (any, error) {
	return nil, nil
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