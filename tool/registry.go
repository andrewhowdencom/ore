package tool

import (
	"fmt"
	"sync"

	"github.com/andrewhowdencom/ore/provider"
)

// Registry is the interface for tool registration and lookup.
type Registry interface {
	// Register adds a tool to the registry.
	Register(name, description string, schema map[string]any, fn ToolFunc) error
	// Tools returns a merged list of all registered tools.
	Tools() []provider.Tool
	// Lookup returns the tool function and true if the tool is registered locally.
	Lookup(name string) (ToolFunc, bool)
	// LookupRemoteSource finds a remote source by its namespace prefix.
	LookupRemoteSource(namespace string) RemoteSource
}

// Option configures a registry via the functional options pattern.
type Option func(*registry)

// WithMCPServer registers a remote tool source with the registry.
// Remote tools are namespaced under the source's Name() prefix.
func WithMCPServer(source RemoteSource) Option {
	return func(r *registry) {
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

// registry is the default in-memory implementation of Registry.
type registry struct {
	mu            sync.RWMutex
	localTools    map[string]*localTool
	remoteSources []RemoteSource
}

// NewRegistry creates an empty tool registry ready for tool registration.
func NewRegistry(opts ...Option) Registry {
	r := &registry{
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
func (r *registry) Register(name, description string, schema map[string]any, fn ToolFunc) error {
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
func (r *registry) Tools() []provider.Tool {
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

// Lookup returns the tool function and true if found.
func (r *registry) Lookup(name string) (ToolFunc, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	lt, ok := r.localTools[name]
	if !ok {
		return nil, false
	}
	return lt.fn, true
}

// LookupRemoteSource finds a remote source by its namespace prefix.
func (r *registry) LookupRemoteSource(namespace string) RemoteSource {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, rs := range r.remoteSources {
		if rs.Name() == namespace {
			return rs
		}
	}
	return nil
}
