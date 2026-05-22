// Package main provides a terminal-based coding assistant built with ore.
//
// It wires together the TUI conduit, a system prompt transform that injects
// a coding-specific persona, and guardrails that enforce formatting rules.
// All composition is done directly in Go — there is no YAML blueprint layer.
//
// Usage:
//
//	export ORE_API_KEY=...
//	export ORE_MODEL=gpt-4o
//	go run ./examples/workshop
//
// Resume an existing thread:
//
//	go run ./examples/workshop --thread <uuid>
//
// With persistent JSON store:
//
//	STORE_DIR=/tmp/ore-store go run ./examples/workshop
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"

	"github.com/andrewhowdencom/ore/cognitive"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/x/provider/openai"
	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/thread"
	"github.com/andrewhowdencom/ore/x/conduit/tui"
	"github.com/andrewhowdencom/ore/x/guardrails"
	"github.com/andrewhowdencom/ore/x/systemprompt"
	"github.com/andrewhowdencom/ore/x/tool"
	"github.com/andrewhowdencom/ore/x/tool/bash"
	"github.com/andrewhowdencom/ore/x/tool/filesystem"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		slog.Error("fatal error", "err", err)
		os.Exit(1)
	}
}

func run() error {
	// Parse command-line flags.
	var threadID string
	flag.StringVar(&threadID, "thread", "", "existing thread UUID to resume")
	flag.Parse()

	// Environment configuration.
	apiKey := os.Getenv("ORE_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("ORE_API_KEY not set")
	}

	modelName := os.Getenv("ORE_MODEL")
	if modelName == "" {
		modelName = "gpt-4o"
	}

	baseURL := os.Getenv("ORE_BASE_URL")

	// Create thread store.
	var store thread.Store
	if storeDir := os.Getenv("STORE_DIR"); storeDir != "" {
		var err error
		store, err = thread.NewJSONStore(storeDir)
		if err != nil {
			return fmt.Errorf("create JSON store: %w", err)
		}
	} else {
		store = thread.NewMemoryStore()
	}

	// Build OpenAI provider.
	var opts []openai.Option
	if baseURL != "" {
		opts = append(opts, openai.WithBaseURL(baseURL))
	}
	prov, err := openai.New(append([]openai.Option{
		openai.WithAPIKey(apiKey),
		openai.WithModel(modelName),
	}, opts...)...)
	if err != nil {
		return fmt.Errorf("create openai provider: %w", err)
	}

	// Step factory: inject system prompt and guardrails as transforms.
	stepFactory := func() (*loop.Step, error) {
		sp, err := systemprompt.New(systemprompt.WithContent(
			"You are a terminal-based coding assistant. " +
				"You help users write, review, refactor, and debug code across any language or framework. " +
				"You have access to filesystem tools (read_file, write_file, edit_file, list_directory, search_files) and a bash tool for running shell commands. " +
				"Use these tools proactively to explore the codebase, make changes, run tests, and verify your work. " +
				"Prefer concise explanations and actionable suggestions.",
		))
		if err != nil {
			return nil, fmt.Errorf("create system prompt transform: %w", err)
		}

		gr, err := guardrails.New(guardrails.WithRules(
			"Always format code in markdown blocks with the correct language tag.",
			"Prefer concise explanations; show code rather than prose where possible.",
			"When suggesting changes, explain the rationale briefly.",
			"Before writing or editing files, verify the target path and confirm the change is intended.",
		))
		if err != nil {
			return nil, fmt.Errorf("create guardrails transform: %w", err)
		}

		// Create tool registry with filesystem and bash functions.
		registry := tool.NewRegistry()
		registry.Register(filesystem.ReadFileTool.Name, filesystem.ReadFileTool.Description, filesystem.ReadFileTool.Schema, filesystem.ReadFile)
		registry.Register(filesystem.WriteFileTool.Name, filesystem.WriteFileTool.Description, filesystem.WriteFileTool.Schema, filesystem.WriteFile)
		registry.Register(filesystem.EditFileTool.Name, filesystem.EditFileTool.Description, filesystem.EditFileTool.Schema, filesystem.EditFile)
		registry.Register(filesystem.ListDirectoryTool.Name, filesystem.ListDirectoryTool.Description, filesystem.ListDirectoryTool.Schema, filesystem.ListDirectory)
		registry.Register(filesystem.SearchFilesTool.Name, filesystem.SearchFilesTool.Description, filesystem.SearchFilesTool.Schema, filesystem.SearchFiles)
		registry.Register(bash.BashTool.Name, bash.BashTool.Description, bash.BashTool.Schema, bash.Bash)

		return loop.New(
			loop.WithTransforms(sp, gr),
			loop.WithHandlers(registry.Handler()),
			loop.WithInvokeOptions(openai.WithTools(registry.Tools())),
		), nil
	}

	// Create session manager with the ReAct cognitive pattern.
	mgr := session.NewManager(store, prov, stepFactory, cognitive.NewTurnProcessor())

	// Create the TUI conduit, passing the thread ID via functional option.
	c, err := tui.New(mgr, tui.WithThreadID(threadID))
	if err != nil {
		return fmt.Errorf("create TUI conduit: %w", err)
	}

	// Start the TUI and block until the user quits (Ctrl+C) or the
	// context is cancelled.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return c.Start(ctx)
}
