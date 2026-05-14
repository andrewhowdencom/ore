// Package main provides a streaming chat REPL demonstrating the ore
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
	"sync"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/cognitive"
	"github.com/andrewhowdencom/ore/thread"
	"github.com/andrewhowdencom/ore/conduit"
	"github.com/andrewhowdencom/ore/conduit/tui"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/provider/openai"
	"github.com/andrewhowdencom/ore/state"
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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

	// Create or load thread.
	var thread *thread.Thread
	if threadID != "" {
		var ok bool
		thread, ok = store.Get(threadID)
		if !ok {
			return fmt.Errorf("thread %q not found", threadID)
		}
	} else {
		var err error
		thread, err = store.Create()
		if err != nil {
			return fmt.Errorf("create thread: %w", err)
		}
		slog.Info("thread started", "id", thread.ID)
	}

	// Build OpenAI provider.
	var opts []openai.Option
	if baseURL != "" {
		opts = append(opts, openai.WithBaseURL(baseURL))
	}
	prov := openai.New(apiKey, modelName, opts...)

	// Step with embedded FanOut for event distribution.
	st := loop.New()
	defer st.Close()

	// Create TUI conduit subscribed to the event stream.
	ch := st.Subscribe("text_delta", "reasoning_delta", "tool_call_delta", "turn_complete")
	s := tui.New(ch)

	// Cognitive pattern.
	react := &cognitive.ReAct{
		Step:     st,
		Provider: prov,
	}

	// Event processing goroutine.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()

		var mu sync.Mutex
		var cancelFunc context.CancelFunc

		for event := range s.Events() {
			switch e := event.(type) {
			case conduit.UserMessageEvent:
				// Record the user's message as a non-inference turn so it
				// appears in the same artifact stream as assistant responses.
				result, err := react.Step.Submit(ctx, thread.State, state.RoleUser, artifact.Text{Content: e.Content})
				if err != nil {
					slog.Error("submit failed", "err", err)
					continue
				}
				_ = result // thread.State is mutated in place
				if err := s.SetStatus(ctx, "thinking..."); err != nil {
					slog.Error("set status failed", "err", err)
				}

				opCtx, cancel := context.WithCancel(ctx)
				mu.Lock()
				cancelFunc = cancel
				mu.Unlock()

				result, err = react.Run(opCtx, thread.State)
				// State is mutable, so result is the same pointer.
				_ = result

				// Save thread state after each turn.
				if err := store.Save(thread); err != nil {
					slog.Error("save thread failed", "err", err)
				}

				mu.Lock()
				cancelFunc = nil
				mu.Unlock()
				cancel()

				if err := s.SetStatus(ctx, ""); err != nil {
					slog.Error("set status failed", "err", err)
				}
				if err != nil {
					slog.Error("react failed", "err", err)
				}

			case conduit.InterruptEvent:
				// Propagate a Ctrl+C cancellation to the ongoing inference turn.
				mu.Lock()
				if cancelFunc != nil {
					cancelFunc()
				}
				mu.Unlock()
			}
		}
	}()

	// Run the TUI. This blocks until the user quits (Ctrl+C).
	if err := s.Run(); err != nil {
		return fmt.Errorf("tui exited: %w", err)
	}

	// Clean shutdown: cancel the context and wait for the event loop to finish.
	cancel()
	wg.Wait()

	return nil
}
