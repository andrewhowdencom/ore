// tui-chat is a reference application demonstrating a streaming chat REPL
// using the tack framework with the surface/tui package.
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
	apiKey := os.Getenv("TACK_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("TACK_API_KEY not set")
	}

	modelName := os.Getenv("TACK_MODEL")
	if modelName == "" {
		modelName = "gpt-4o"
	}

	baseURL := os.Getenv("TACK_BASE_URL")

	// Create TUI surface.
	s := tui.New()

	// Build OpenAI provider.
	var opts []openai.Option
	if baseURL != "" {
		opts = append(opts, openai.WithBaseURL(baseURL))
	}
	prov := openai.New(apiKey, modelName, opts...)

	// Step with output events.
	outputCh := make(chan loop.OutputEvent, 100)
	st := loop.New(loop.WithOutput(outputCh))

	// Goroutine: route Step output to TUI surface.
	go func() {
		for event := range outputCh {
			switch e := event.(type) {
			case loop.DeltaEvent:
				if err := s.RenderDelta(ctx, e.Delta); err != nil {
					slog.Error("render delta failed", "err", err)
				}
			case loop.TurnCompleteEvent:
				if err := s.RenderTurn(ctx, e.Turn); err != nil {
					slog.Error("render turn failed", "err", err)
				}
			}
		}
	}()

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
				mem.Append(state.RoleUser, artifact.Text{Content: e.Content})
				if err := s.SetStatus(ctx, "thinking..."); err != nil {
					slog.Error("set status failed", "err", err)
				}

				opCtx, cancel := context.WithCancel(ctx)
				mu.Lock()
				cancelFunc = cancel
				mu.Unlock()

				result, err := react.Run(opCtx, mem)
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
