// Package bash provides a shell command execution tool for the ore tool
// extension.
//
// It exports a pre-built Bash tool function together with its
// provider.Tool JSON-schema descriptor, so applications can register it in a
// tool.Registry without defining the logic inline.
//
// WARNING: The bash tool executes arbitrary shell commands on the host
// system. There is no sandbox, allowlist, or confirmation prompt. Register
// this tool only in trusted, isolated environments.
//
// Usage:
//
//	registry := tool.NewRegistry()
//	registry.Register(bash.BashTool.Name, bash.BashTool.Description, bash.BashTool.Schema, bash.Bash)
//
//	// Registry.Tools() is the single source of truth for the provider.
//	tools := registry.Tools()
//
// See also: x/tool/bash/bash.go for the tool implementation.
package bash