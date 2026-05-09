// single-turn-cli is a reference application demonstrating composition of the
// tack loop.Step with an OpenAI-compatible provider adapter.
package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/andrewhowdencom/tack/artifact"
	"github.com/andrewhowdencom/tack/loop"
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

	// Tool calling example (uncomment to enable):
	//
	//   registry := tool.NewRegistry()
	//   registry.Register("calculator", func(ctx context.Context, args map[string]any) (any, error) {
	//       return "42", nil
	//   })
	//   p := openai.New(apiKey, model, opts...)
	//   p.SetTools([]provider.Tool{
	//       {Name: "calculator", Description: "A simple calculator", Schema: map[string]any{"type": "object"}},
	//   })
	//   s := loop.New(loop.WithHandlers(registry.Handler()))
	//
	// Note: to use tools, loop until the assistant responds with text rather
	// than a single turn. See examples/calculator for a complete example.

	// Execute a single loop turn.
	s := loop.New()
	_, err := s.Turn(ctx, mem, p)
	if err != nil {
		return fmt.Errorf("turn failed: %w", err)
	}

	// Print assistant artifacts from the response.
	turns := mem.Turns()
	if len(turns) == 0 {
		return fmt.Errorf("no turns in state")
	}
	last := turns[len(turns)-1]
	for _, art := range last.Artifacts {
		switch a := art.(type) {
		case artifact.Text:
			fmt.Println(a.Content)
		case artifact.Reasoning:
			fmt.Printf("--- reasoning ---\n%s\n", a.Content)
		case artifact.ToolCall:
			fmt.Printf("--- tool_call: %s ---\n%s\n", a.Name, a.Arguments)
		case artifact.Image:
			fmt.Printf("--- image ---\n%s\n", a.URL)
		default:
			fmt.Printf("--- %s ---\n[unsupported artifact type]\n", art.Kind())
		}
	}

	return nil
}
