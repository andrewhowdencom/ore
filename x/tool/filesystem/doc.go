// Package filesystem provides reusable filesystem tool implementations for the
// ore tool extension.
//
// It exports pre-built ReadFile, WriteFile, EditFile, ListDirectory and
// SearchFiles tool functions together with their provider.Tool JSON-schema
// descriptors, so applications can register them in a tool.Registry without
// defining the logic inline.
//
// Usage:
//
//	registry := tool.NewRegistry()
//	registry.Register(filesystem.ReadFileTool.Name, filesystem.ReadFileTool.Description, filesystem.ReadFileTool.Schema, filesystem.ReadFile)
//	registry.Register(filesystem.WriteFileTool.Name, filesystem.WriteFileTool.Description, filesystem.WriteFileTool.Schema, filesystem.WriteFile)
//	registry.Register(filesystem.EditFileTool.Name, filesystem.EditFileTool.Description, filesystem.EditFileTool.Schema, filesystem.EditFile)
//	registry.Register(filesystem.ListDirectoryTool.Name, filesystem.ListDirectoryTool.Description, filesystem.ListDirectoryTool.Schema, filesystem.ListDirectory)
//	registry.Register(filesystem.SearchFilesTool.Name, filesystem.SearchFilesTool.Description, filesystem.SearchFilesTool.Schema, filesystem.SearchFiles)
//
//	// Registry.Tools() is the single source of truth for the provider.
//	tools := registry.Tools()
package filesystem
