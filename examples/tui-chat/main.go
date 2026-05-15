// Package main provides an interactive chat REPL demonstrating the ore
// framework. It wires together the ReAct cognitive pattern, the loop.Step
// primitive for turn orchestration, and the conduit/tui package for
// terminal interaction.
//
// Usage:
//
//	go run ./examples/tui-chat
//
// Resume an existing thread:
//
//	go run ./examples/tui-chat --thread <uuid>
//
// With persistent JSON store:
//
//	STORE_DIR=/tmp/ore-store go run ./examples/tui-chat
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/andrewhowdencom/ore/cognitive"
	"github.com/andrewhowdencom/ore/conduit/tui"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/provider/openai"
	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/thread"
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
	prov := openai.New(apiKey, modelName, opts...)

	// Step factory for the manager.
	stepFactory := func() *loop.Step {
		return loop.New()
	}

	// Create session manager with the ReAct cognitive pattern.
	mgr := session.NewManager(store, prov, stepFactory, cognitive.NewTurnProcessor())

	// Obtain a session — resume an existing thread or create a new one.
	var stream *session.Stream
	var err error
	if threadID != "" {
		stream, err = mgr.Attach(threadID)
		if err != nil {
			return fmt.Errorf("attach to thread %q: %w", threadID, err)
		}
	} else {
		stream, err = mgr.Create()
		if err != nil {
			return fmt.Errorf("create session: %w", err)
		}
		slog.Info("thread started", "id", stream.ID())
	}

	// Create TUI conduit composed with the session.
	// The TUI manages its own subscription and event loop; do not call
	// stream.Subscribe or stream.Process from application code.
	s := tui.New(stream)

	// Run the TUI. This blocks until the user quits (Ctrl+C).
	// s.Run() closes the events channel on return so background
	// goroutines exit cleanly.
	if err := s.Run(); err != nil {
		return fmt.Errorf("tui exited: %w", err)
	}

	return nil
}
