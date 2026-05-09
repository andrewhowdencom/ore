// tui-chat is a reference application demonstrating a streaming chat REPL
// using the tack framework with the surface/tui package.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/andrewhowdencom/tack/loop"
	"github.com/andrewhowdencom/tack/orchestrate"
	"github.com/andrewhowdencom/tack/provider/openai"
	"github.com/andrewhowdencom/tack/state"
	"github.com/andrewhowdencom/tack/surface/tui"
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

	// Tool calling example (uncomment this block and comment out the provider
	// and step setup immediately above and below it):
	//
	//   registry := tool.NewRegistry()
	//   registry.Register("calculator", func(ctx context.Context, args map[string]any) (any, error) {
	//       return "42", nil
	//   })
	//   prov := openai.New(apiKey, modelName, opts...)
	//   _ = prov.SetTools([]provider.Tool{
	//       {Name: "calculator", Description: "A simple calculator", Schema: map[string]any{"type": "object"}},
	//   })
	//   st := loop.New(loop.WithSurface(s), loop.WithHandlers(registry.Handler()))
	//
	// The ReAct orchestrator automatically loops while tool calls are in flight.
	// See examples/calculator for a standalone tool-calling example.

	// Compose framework layers.
	st := loop.New(loop.WithSurface(s))
	orchestrator := &orchestrate.ReAct{
		State:    &state.Memory{},
		Step:     st,
		Surface:  s,
		Provider: prov,
	}

	// Run orchestrator in a background goroutine.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := orchestrator.Run(ctx); err != nil {
			slog.Error("orchestrator exited", "err", err)
		}
	}()

	// Run the TUI. This blocks until the user quits (Ctrl+C).
	if err := s.Run(); err != nil {
		return fmt.Errorf("tui exited: %w", err)
	}

	// Clean shutdown: cancel the orchestrator context and wait for it to finish.
	cancel()
	wg.Wait()

	return nil
}
