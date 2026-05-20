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
//	registry.Register(ReadFileTool.Name, ReadFileTool.Description, ReadFileTool.Schema, ReadFile)
//	registry.Register(WriteFileTool.Name, WriteFileTool.Description, WriteFileTool.Schema, WriteFile)
//	registry.Register(EditFileTool.Name, EditFileTool.Description, EditFileTool.Schema, EditFile)
//	registry.Register(ListDirectoryTool.Name, ListDirectoryTool.Description, ListDirectoryTool.Schema, ListDirectory)
//	registry.Register(SearchFilesTool.Name, SearchFilesTool.Description, SearchFilesTool.Schema, SearchFiles)
//
//	// Registry.Tools() is the single source of truth for the provider.
//	tools := registry.Tools()
//
// See also: x/tool/filesystem/filesystem.go for the tool implementations.
package filesystem
