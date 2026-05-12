// Package main provides a streaming chat REPL demonstrating the ore
// framework. It wires together the ReAct cognitive pattern, the loop.Step
// primitive for turn orchestration, and the surface/tui package for
// terminal interaction.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/cognitive"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/provider/openai"
	"github.com/andrewhowdencom/ore/state"
	"github.com/andrewhowdencom/ore/surface"
	"github.com/andrewhowdencom/ore/surface/tui"
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

	// Architecture:
	//   * `react` (cognitive.ReAct) drives the inference loop using the
	//     provider and the Step primitive.
	//   * `Step` emits streaming artifacts and turn-complete events that
	//     the TUI surface consumes.
	//   * The TUI surface forwards user input back as UserMessageEvents,
	//     which are fed into `Step.Submit` to keep the conversation
	//     history consistent.

	// Build OpenAI provider.
	var opts []openai.Option
	if baseURL != "" {
		opts = append(opts, openai.WithBaseURL(baseURL))
	}
	prov := openai.New(apiKey, modelName, opts...)

	// Step with embedded FanOut for event distribution.
	st := loop.New()
	defer st.Close()

	// Create TUI surface subscribed to the event stream.
	ch := st.Subscribe("text_delta", "reasoning_delta", "tool_call_delta", "turn_complete")
	s := tui.New(ch)

	// Cognitive pattern.
	react := &cognitive.ReAct{
		Step:     st,
		Provider: prov,
	}

	// Conversation state.
	mem := &state.Memory{}

	// Event processing goroutine.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()

		var mu sync.Mutex
		var cancelFunc context.CancelFunc

		for event := range s.Events() {
			switch e := event.(type) {
			case surface.UserMessageEvent:
				// Record the user's message as a non-inference turn so it
				// appears in the same artifact stream as assistant responses.
				result, err := react.Step.Submit(ctx, mem, state.RoleUser, artifact.Text{Content: e.Content})
				if err != nil {
					slog.Error("submit failed", "err", err)
					continue
				}
				mem = result.(*state.Memory)
				if err := s.SetStatus(ctx, "thinking..."); err != nil {
					slog.Error("set status failed", "err", err)
				}

				opCtx, cancel := context.WithCancel(ctx)
				mu.Lock()
				cancelFunc = cancel
				mu.Unlock()

				result, err = react.Run(opCtx, mem)
				// State is mutable, so result is the same pointer; reassign for clarity.
				mem = result.(*state.Memory)

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

			case surface.InterruptEvent:
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
