// Package filesystem provides reusable filesystem tool implementations for the
// ore tool extension.
//
// It exports pre-built ReadFile, WriteFile, EditFile, ListDirectory and
// SearchFiles tool functions together with their tool.Tool JSON-schema
// descriptors, so applications can register them in a tool.Registry without
// defining the logic inline.
//
// Usage:
//
//	registry := tool.NewRegistry()
//	if err := registry.Register(ReadFileTool.Name, ReadFileTool.Description, ReadFileTool.Schema, ReadFile); err != nil {
//	    ...
//	}
//	if err := registry.Register(WriteFileTool.Name, WriteFileTool.Description, WriteFileTool.Schema, WriteFile); err != nil {
//	    ...
//	}
//	if err := registry.Register(EditFileTool.Name, EditFileTool.Description, EditFileTool.Schema, EditFile); err != nil {
//	    ...
//	}
//	if err := registry.Register(ListDirectoryTool.Name, ListDirectoryTool.Description, ListDirectoryTool.Schema, ListDirectory); err != nil {
//	    ...
//	}
//	if err := registry.Register(SearchFilesTool.Name, SearchFilesTool.Description, SearchFilesTool.Schema, SearchFiles); err != nil {
//	    ...
//	}
//
//	// Registry.Tools() is the single source of truth for the provider.
//	tools := registry.Tools()
//
// See also: x/tool/filesystem/filesystem.go for the tool implementations.
package filesystem
