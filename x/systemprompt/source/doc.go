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
// Reading a single static prompt file:
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
//
// Composable prompt fragments from multiple sources:
//
//	transform, _ := systemprompt.New(
//	    systemprompt.WithContentFunc(source.AgentsMD("/project/subdir")),
//	    systemprompt.WithContentFunc(source.Harness("my-agent")),
//	    systemprompt.WithContentFunc(source.Model("gpt-4o")),
//	    systemprompt.WithContentFunc(source.Provider("openai")),
//	)
//
// # Loading Agent Definitions
//
// source.File is appropriate for non-agent prompt content (tool
// descriptions, custom instructions, project guidelines), but it is the
// wrong tool for loading agent definition files. Stacking multiple
// source.File("<dir>/<agent>.md") calls produces a system prompt with
// contradictory "## Identity" sections; see the "Multi-Identity
// Stacking" section of x/systemprompt's package documentation.
//
// Use source.Agent and source.AgentReferenceIndex instead. The active
// agent's full body is in context, and the other available agents are
// summarised by name + description so the LLM knows they exist without
// being given conflicting instructions:
//
//	dir := "/path/to/agents"
//	activeName := "build"
//
//	transform, _ := systemprompt.New(
//	    systemprompt.WithContentFuncs(
//	        source.Agent(dir, activeName),
//	        source.AgentReferenceIndex(dir, activeName),
//	    ),
//	)
//
// source.Agent alone (without the reference index) is also valid when
// the LLM should not be aware of other available agents:
//
//	transform, _ := systemprompt.New(
//	    systemprompt.WithContentFunc(source.Agent(dir, "build")),
//	)
package source
