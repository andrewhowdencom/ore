// single-turn-cli is a reference application demonstrating composition of the
// tack core loop with an OpenAI-compatible provider adapter.
package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/andrewhowdencom/tack/artifact"
	"github.com/andrewhowdencom/tack/core"
	"github.com/andrewhowdencom/tack/provider/openai"
	"github.com/andrewhowdencom/tack/state"
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
	ctx := context.Background()

	// Read user message from command-line arguments or stdin.
	message := strings.Join(os.Args[1:], " ")
	if message == "" {
		slog.Info("reading from stdin...")
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			message = scanner.Text()
		}
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}
	}

	if message == "" {
		return fmt.Errorf("no message provided")
	}

	// Environment configuration.
	apiKey := os.Getenv("TACK_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("TACK_API_KEY not set")
	}

	model := os.Getenv("TACK_MODEL")
	if model == "" {
		model = "gpt-4o"
	}

	baseURL := os.Getenv("TACK_BASE_URL")

	// Build state with the user message.
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: message})

	// Build provider.
	var opts []openai.Option
	if baseURL != "" {
		opts = append(opts, openai.WithBaseURL(baseURL))
	}
	p := openai.New(apiKey, model, opts...)

	// Execute a single core loop turn.
	loop := &core.Loop{}
	_, err := loop.Turn(ctx, mem, p)
	if err != nil {
		return fmt.Errorf("turn failed: %w", err)
	}

	// Print assistant text artifacts from the response.
	turns := mem.Turns()
	if len(turns) == 0 {
		return fmt.Errorf("no turns in state")
	}
	last := turns[len(turns)-1]
	for _, art := range last.Artifacts {
		if text, ok := art.(artifact.Text); ok {
			fmt.Println(text.Content)
		}
	}

	return nil
}
