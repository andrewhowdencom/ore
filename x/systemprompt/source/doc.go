// Package source provides factory functions that return func() string closures
// compatible with systemprompt.WithContentFunc, bridging content acquisition
// (files, directory traversal) and the composable system prompt transform.
//
// The core x/systemprompt package is intentionally minimal — it only provides
// composable content functions via WithContentFunc and WithContextContentFunc.
// It has no opinion about where content comes from (files, HTTP, environment
// variables, etc.). This package fills that gap for the common case of reading
// local files and discovering AGENTS.md / CLAUDE.md instruction files by walking
// parent directories.
//
// # Usage
//
//	import (
//	    "github.com/andrewhowdencom/ore/x/systemprompt"
//	    "github.com/andrewhowdencom/ore/x/systemprompt/source"
//	)
//
//	transform, _ := systemprompt.New(
//	    systemprompt.WithContentFunc(source.File("prompt.txt")),
//	)
//
// Discovering AGENTS.md files up the directory tree:
//
//	transform, _ := systemprompt.New(
//	    systemprompt.WithContentFunc(source.AgentsMD("/project/subdir")),
//	)
package source
