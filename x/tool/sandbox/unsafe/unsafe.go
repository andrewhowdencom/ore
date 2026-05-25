// Package unsafe provides a no-op sandbox implementation that intentionally
// does NOT enforce any isolation. It is an escape hatch for sandboxless
// contexts (local development, quick prototypes, and examples) where a real
// sandbox implementation is unavailable.
//
// WARNING: This sandbox performs no security checks. Use ONLY for local
// development, testing, or examples. Do not use in production or where
// isolation is required.
//
// This package deliberately implements only tool.Sandbox (the base interface
// with Name). It does NOT implement tool.FileSandbox or tool.ExecSandbox.
// This means:
//   - Bash falls through to raw os/exec with no working directory constraints
//   - Filesystem tools pass paths through as-is (same behavior as nil sandbox)
//   - Calculator/skills tools ignore it anyway
//
// This keeps the "unsafe" semantics explicit: the type name, package path, and
// documentation all signal that this is an escape hatch, not a security
// boundary.
//
// Import alias recommended due to collision with Go standard library "unsafe":
//
//	import unsandbox "github.com/andrewhowdencom/ore/x/tool/sandbox/unsafe"
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

// New creates a new unsafe sandbox with the given name and returns it as a
// tool.Sandbox interface. Returning the interface hides the concrete
// implementation and mirrors the factory pattern used by other sandbox
// constructors.
func New(name string) tool.Sandbox {
	return &Sandbox{name: name}
}
