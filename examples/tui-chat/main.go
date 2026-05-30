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
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/andrewhowdencom/ore/cognitive"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/x/conduit/tui"
	"github.com/andrewhowdencom/ore/x/provider/openai"
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
	var listThreads bool
	flag.StringVar(&threadID, "thread", "", "existing thread UUID to resume")
	flag.BoolVar(&listThreads, "list-threads", false, "list all persisted threads and exit")
	flag.Parse()

	if listThreads {
		// Create thread store (respecting STORE_DIR env var).
		var store session.Store
		if storeDir := os.Getenv("STORE_DIR"); storeDir != "" {
			var err error
			store, err = session.NewJSONStore(storeDir)
			if err != nil {
				return fmt.Errorf("create JSON store: %w", err)
			}
		} else {
			store = session.NewMemoryStore()
		}

		threads, err := store.List()
		if err != nil {
			return fmt.Errorf("list threads: %w", err)
		}

		// Sort by UpdatedAt descending (most recently active first).
		sort.Slice(threads, func(i, j int) bool {
			return threads[i].UpdatedAt.After(threads[j].UpdatedAt)
		})

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tCreatedAt\tUpdatedAt")
		for _, t := range threads {
			fmt.Fprintf(w, "%s\t%s\t%s\n",
				t.ID,
				t.CreatedAt.Format(time.RFC3339),
				t.UpdatedAt.Format(time.RFC3339),
			)
		}
		w.Flush()

		os.Exit(0)
	}

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
	var store session.Store
	if storeDir := os.Getenv("STORE_DIR"); storeDir != "" {
		var err error
		store, err = session.NewJSONStore(storeDir)
		if err != nil {
			return fmt.Errorf("create JSON store: %w", err)
		}
	} else {
		store = session.NewMemoryStore()
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

	// Step factory for the manager.
	stepFactory := func(thr *session.Thread) (*loop.Step, error) {
		return loop.New(loop.WithOnEmit(func(ctx context.Context, event loop.OutputEvent) {
			if tc, ok := event.(loop.TurnCompleteEvent); ok {
				thr.State.Append(tc.Turn.Role, tc.Turn.Artifacts...)
			}
		})), nil
	}

	// Create session manager with the ReAct cognitive pattern.
	mgr := session.NewManager(store, prov, stepFactory, cognitive.NewTurnProcessor())

	// Create the TUI conduit, passing the thread ID via functional option.
	// The TUI creates or attaches to the session internally on Start.
	c, err := tui.New(mgr, tui.WithName("tui-chat"), tui.WithThreadID(threadID))
	if err != nil {
		return fmt.Errorf("create TUI conduit: %w", err)
	}

	// Start the TUI and block until the user quits (Ctrl+C) or the
	// context is cancelled.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return c.Start(ctx)
}
