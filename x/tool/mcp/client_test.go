package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	mcptypes "github.com/mark3labs/mcp-go/mcp"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Compile-time interface check.
var _ tool.RemoteSource = (*Client)(nil)

// mockCaller implements the caller interface for unit tests.
type mockCaller struct {
	result *mcptypes.CallToolResult
	err    error
}

func (m *mockCaller) CallTool(ctx context.Context, request mcptypes.CallToolRequest) (*mcptypes.CallToolResult, error) {
	return m.result, m.err
}

func TestOptions_WithName(t *testing.T) {
	cfg := &config{}
	WithName("filesystem")(cfg)
	assert.Equal(t, "filesystem", cfg.name)
}

func TestOptions_WithStdio(t *testing.T) {
	cfg := &config{}
	WithStdio("python", "server.py")(cfg)
	assert.Equal(t, "python", cfg.command)
	assert.Equal(t, []string{"server.py"}, cfg.args)
}

func TestOptions_WithSSE(t *testing.T) {
	cfg := &config{}
	WithSSE("https://mcp.example.com/sse")(cfg)
	assert.Equal(t, "https://mcp.example.com/sse", cfg.url)
}

func TestOptions_WithBearerToken(t *testing.T) {
	cfg := &config{}
	WithBearerToken("secret123")(cfg)
	assert.Equal(t, "secret123", cfg.bearerToken)
}

func TestOptions_WithHeader(t *testing.T) {
	cfg := &config{}
	WithHeader("X-Custom", "value")(cfg)
	assert.Equal(t, "value", cfg.headers["X-Custom"])
}

func TestOptions_WithHeader_Multiple(t *testing.T) {
	cfg := &config{}
	WithHeader("X-First", "one")(cfg)
	WithHeader("X-Second", "two")(cfg)
	assert.Equal(t, "one", cfg.headers["X-First"])
	assert.Equal(t, "two", cfg.headers["X-Second"])
}

func TestBuildSSEOptions_WithBearerToken(t *testing.T) {
	cfg := &config{
		bearerToken: "token",
	}
	opts := buildSSEOptions(cfg)
	require.Len(t, opts, 1)
}

func TestBuildSSEOptions_WithBearerTokenAndHeaders(t *testing.T) {
	cfg := &config{
		bearerToken: "token",
		headers:     map[string]string{"X-Custom": "value"},
	}
	opts := buildSSEOptions(cfg)
	require.Len(t, opts, 1)
}

func TestNewClient_MissingTransport(t *testing.T) {
	_, err := NewClient(WithName("test"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no transport configured")
}

func TestToolInputSchemaToMap(t *testing.T) {
	schema := mcptypes.ToolInputSchema{
		Type:       "object",
		Properties: map[string]any{"a": map[string]any{"type": "number"}},
		Required:   []string{"a"},
	}

	m, err := toolInputSchemaToMap(schema)
	require.NoError(t, err)
	assert.Equal(t, "object", m["type"])
	assert.Contains(t, m, "properties")
	assert.Contains(t, m, "required")
}

func TestToolInputSchemaToMap_Empty(t *testing.T) {
	schema := mcptypes.ToolInputSchema{}
	m, err := toolInputSchemaToMap(schema)
	require.NoError(t, err)
	assert.NotNil(t, m)
}

func TestToolInputSchemaToMap_RoundTrip(t *testing.T) {
	schema := mcptypes.ToolInputSchema{
		Type:       "object",
		Properties: map[string]any{"path": map[string]any{"type": "string"}},
		Required:   []string{"path"},
	}

	m, err := toolInputSchemaToMap(schema)
	require.NoError(t, err)

	data, err := json.Marshal(m)
	require.NoError(t, err)

	var back mcptypes.ToolInputSchema
	require.NoError(t, json.Unmarshal(data, &back))

	assert.Equal(t, schema.Type, back.Type)
	assert.Equal(t, schema.Required, back.Required)
}

func TestClient_Name(t *testing.T) {
	c := &Client{name: "filesystem"}
	assert.Equal(t, "filesystem", c.Name())
}

func TestClient_Tools_DefensiveCopy(t *testing.T) {
	c := &Client{
		name:  "filesystem",
		tools: []provider.Tool{{Name: "read_file", Description: "Read"}},
	}

	t1 := c.Tools()
	t2 := c.Tools()

	require.Len(t, t1, 1)
	require.Len(t, t2, 1)

	// Mutating one copy should not affect the other.
	t1[0].Name = "modified"
	assert.Equal(t, "read_file", t2[0].Name)
	assert.Equal(t, "read_file", c.tools[0].Name)
}

func TestClient_Call_StructuredContent(t *testing.T) {
	c := &Client{
		name: "test",
		caller: &mockCaller{
			result: &mcptypes.CallToolResult{
				StructuredContent: map[string]any{"lines": 42},
				Content:           []mcptypes.Content{},
			},
		},
	}

	result, err := c.Call(context.Background(), "read_file", map[string]any{"path": "/tmp/test"})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"lines": 42}, result)
}

func TestClient_Call_SingleTextContent(t *testing.T) {
	c := &Client{
		name: "test",
		caller: &mockCaller{
			result: &mcptypes.CallToolResult{
				Content: []mcptypes.Content{
					mcptypes.TextContent{Text: "hello"},
				},
			},
		},
	}

	result, err := c.Call(context.Background(), "echo", map[string]any{"msg": "hi"})
	require.NoError(t, err)
	assert.Equal(t, "hello", result)
}

func TestClient_Call_MultipleTextContent(t *testing.T) {
	c := &Client{
		name: "test",
		caller: &mockCaller{
			result: &mcptypes.CallToolResult{
				Content: []mcptypes.Content{
					mcptypes.TextContent{Text: "line1"},
					mcptypes.TextContent{Text: "line2"},
				},
			},
		},
	}

	result, err := c.Call(context.Background(), "multi", nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"line1", "line2"}, result)
}

func TestClient_Call_Error(t *testing.T) {
	c := &Client{
		name: "test",
		caller: &mockCaller{
			result: &mcptypes.CallToolResult{
				IsError: true,
				Content: []mcptypes.Content{
					mcptypes.TextContent{Text: "file not found"},
				},
			},
		},
	}

	_, err := c.Call(context.Background(), "read_file", map[string]any{"path": "/missing"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "file not found")
}

func TestClient_Call_Error_NoContent(t *testing.T) {
	c := &Client{
		name: "test",
		caller: &mockCaller{
			result: &mcptypes.CallToolResult{
				IsError: true,
				Content: []mcptypes.Content{},
			},
		},
	}

	_, err := c.Call(context.Background(), "fail", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MCP tool call failed")
}

func TestClient_Call_CallerError(t *testing.T) {
	c := &Client{
		name:   "test",
		caller: &mockCaller{err: errors.New("network down")},
	}

	_, err := c.Call(context.Background(), "read_file", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "network down")
}
