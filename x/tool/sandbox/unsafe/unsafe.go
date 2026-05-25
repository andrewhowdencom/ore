// Package unsafe provides a no-op sandbox implementation that intentionally does
// NOT enforce any isolation. It is an escape hatch for sandboxless contexts
// (local development, quick prototypes, and examples) where a real sandbox
// implementation is unavailable.
//
// This package deliberately implements only tool.Sandbox (Name). It does NOT
// implement tool.FileSandbox or tool.ExecSandbox. This means:
//   - Bash falls through to raw os/exec with no working directory constraints
//   - Filesystem tools pass paths through as-is (same behavior as nil sandbox)
//   - Calculator/skills tools ignore it anyway
//
// This keeps the "unsafe" semantics explicit: the type name, package path, and
// documentation all signal that this is an escape hatch, not a security boundary.
package unsafe

import "github.com/andrewhowdencom/ore/tool"

// Sandbox is a no-op sandbox that provides no isolation.
type Sandbox struct {
	name string
}

// Name returns the sandbox name.
func (s *Sandbox) Name() string {
	return s.name
}

// Compile-time check that Sandbox implements tool.Sandbox.
var _ tool.Sandbox = (*Sandbox)(nil)

// New creates a new unsafe Sandbox with the given name.
func New(name string) tool.Sandbox {
	return &Sandbox{name: name}
}
