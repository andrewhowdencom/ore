// Package unsafe provides a tool.Sandbox implementation that offers no
// isolation. It is intended for local development, prototyping, and examples
// where real sandboxing is unavailable.
//
// This package deliberately does not implement tool.FileSandbox or
// tool.ExecSandbox. When passed to the bash tool, it causes execution to fall
// through to raw os/exec with no working directory constraints. When passed to
// filesystem tools, paths pass through unchanged (same behavior as a nil
// sandbox).
//
// Warning: This package provides no isolation. Do not use it in production.
package unsafe

import "github.com/andrewhowdencom/ore/tool"

// Sandbox is a tool.Sandbox implementation that provides no isolation.
// It satisfies the interface requirement for tools that refuse nil sandboxes
// (e.g., bash) while deliberately not implementing FileSandbox or ExecSandbox,
// so execution falls through to the host environment.
type Sandbox struct {
	name string
}

// Name returns the sandbox name.
func (s *Sandbox) Name() string {
	return s.name
}

// New creates a new Sandbox with the given name.
func New(name string) tool.Sandbox {
	return &Sandbox{name: name}
}
