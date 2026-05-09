package tool

import "sync"

// Registry maps tool names to their implementations.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]ToolFunc
}

// NewRegistry creates an empty tool registry.
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]ToolFunc),
	}
}

// Register adds a tool to the registry. If a tool with the same name already
// exists, it is overwritten.
func (r *Registry) Register(name string, fn ToolFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tools == nil {
		r.tools = make(map[string]ToolFunc)
	}
	r.tools[name] = fn
}

// lookup returns the tool function and true if found.
func (r *Registry) lookup(name string) (ToolFunc, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	fn, ok := r.tools[name]
	return fn, ok
}

// Handler returns a Handler backed by this registry.
func (r *Registry) Handler() *Handler {
	return &Handler{registry: r}
}
