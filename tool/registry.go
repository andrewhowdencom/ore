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

// SandboxRegistry extends Registry with sandbox management capabilities.
// Handlers type-assert the registry to SandboxRegistry to resolve sandboxes
// per tool call. If the registry does not implement this interface, all tool
// calls receive a nil sandbox.
type SandboxRegistry interface {
	Registry
	// RegisterSandbox adds a named sandbox to the registry. If a sandbox with
	// the same name already exists, it is overwritten.
	RegisterSandbox(name string, sb Sandbox)
	// SetDefaultSandbox sets the default sandbox used when no explicit
	// "sandbox" argument is present in a tool call.
	SetDefaultSandbox(sb Sandbox)
	// LookupSandbox returns the named sandbox and true if found.
	LookupSandbox(name string) (Sandbox, bool)
	// DefaultSandbox returns the default sandbox (may be nil).
	DefaultSandbox() Sandbox
}

// Option is a functional-options setter that configures a Registry instance
// at creation time.
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
	mu             sync.RWMutex
	localTools     map[string]*localTool
	remoteSources  []RemoteSource
	sandboxes      map[string]Sandbox
	defaultSandbox Sandbox
}

// Compile-time assertion that registry implements SandboxRegistry.
var _ SandboxRegistry = (*registry)(nil)

// NewRegistry creates an empty tool registry ready for tool registration.
// The returned registry is not safe for concurrent use; callers must
// serialize accesses or provide their own synchronization.
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
	if name == "" {
		return fmt.Errorf("tool name cannot be empty")
	}
	if fn == nil {
		return fmt.Errorf("tool function cannot be nil")
	}
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

// RegisterSandbox adds a named sandbox to the registry.
func (r *registry) RegisterSandbox(name string, sb Sandbox) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sandboxes == nil {
		r.sandboxes = make(map[string]Sandbox)
	}
	r.sandboxes[name] = sb
}

// SetDefaultSandbox sets the default sandbox for tool calls.
func (r *registry) SetDefaultSandbox(sb Sandbox) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.defaultSandbox = sb
}

// LookupSandbox returns the named sandbox and true if found.
func (r *registry) LookupSandbox(name string) (Sandbox, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sb, ok := r.sandboxes[name]
	return sb, ok
}

// DefaultSandbox returns the default sandbox (may be nil).
func (r *registry) DefaultSandbox() Sandbox {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.defaultSandbox
}
