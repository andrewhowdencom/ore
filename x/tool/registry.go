package tool

import (
	"context"
	"fmt"
	"sync"

	"github.com/andrewhowdencom/ore/provider"
)

// RemoteSource represents an external source of tools (e.g., an MCP server).
// The Registry consumes this interface without importing the concrete MCP
// package, allowing clean extension without import cycles.
type RemoteSource interface {
	// Name returns the namespace prefix for tools from this source.
	Name() string
	// Tools returns the list of tools available from this source (un-namespaced).
	Tools() []provider.Tool
	// Call invokes a tool by name with the given arguments.
	Call(ctx context.Context, name string, args map[string]any) (any, error)
}

// Option configures a Registry via the functional options pattern.
type Option func(*Registry)

// WithMCPServer registers a remote tool source with the registry.
// Remote tools are namespaced under the source's Name() prefix.
func WithMCPServer(source RemoteSource) Option {
	return func(r *Registry) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.remoteSources = append(r.remoteSources, source)
	}
}

type localTool struct {
	name        string
	description string
	schema      map[string]any
	fn          ToolFunc
}

// Registry maps tool names to their implementations and metadata.
// It can compose remote tool sources discovered from MCP servers alongside
// local Go functions.
type Registry struct {
	mu            sync.RWMutex
	localTools    map[string]*localTool
	remoteSources []RemoteSource
}

// NewRegistry creates an empty tool registry ready for tool registration.
// Register tools by name, then call Handler() to obtain a loop.Handler that
// executes them against incoming artifact.ToolCall values.
func NewRegistry(opts ...Option) *Registry {
	r := &Registry{
		localTools: make(map[string]*localTool),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Register adds a tool to the registry. If a tool with the same name already
// exists, it is overwritten. The description and schema are captured so the
// registry can produce a unified []provider.Tool list for provider adapters.
func (r *Registry) Register(name, description string, schema map[string]any, fn ToolFunc) error {
	if err := ValidateSchema(schema); err != nil {
		return fmt.Errorf("register tool %q: %w", name, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.localTools == nil {
		r.localTools = make(map[string]*localTool)
	}
	r.localTools[name] = &localTool{
		name:        name,
		description: description,
		schema:      schema,
		fn:          fn,
	}
	return nil
}

// Tools returns a merged list of all registered tools, including local tools
// and remote tools from all registered MCP servers. Local tools are returned
// without a prefix. Remote tools are namespaced with their source prefix
// (e.g., "filesystem/read_file").
func (r *Registry) Tools() []provider.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tools := make([]provider.Tool, 0, len(r.localTools))

	for _, lt := range r.localTools {
		tools = append(tools, provider.Tool{
			Name:        lt.name,
			Description: lt.description,
			Schema:      lt.schema,
		})
	}

	for _, rs := range r.remoteSources {
		for _, rt := range rs.Tools() {
			tools = append(tools, provider.Tool{
				Name:        rs.Name() + "/" + rt.Name,
				Description: rt.Description,
				Schema:      rt.Schema,
			})
		}
	}

	return tools
}

// lookup returns the tool function and true if found.
func (r *Registry) lookup(name string) (ToolFunc, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	lt, ok := r.localTools[name]
	if !ok {
		return nil, false
	}
	return lt.fn, true
}

// lookupRemoteSource finds a remote source by its namespace prefix.
func (r *Registry) lookupRemoteSource(namespace string) RemoteSource {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, rs := range r.remoteSources {
		if rs.Name() == namespace {
			return rs
		}
	}
	return nil
}

// Handler returns a Handler backed by this registry.
func (r *Registry) Handler() *Handler {
	return &Handler{registry: r}
}