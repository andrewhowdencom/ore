// calculator is a reference application demonstrating tool calling with ore.
// It registers "add" and "multiply" tools, configures an OpenAI provider with
// them, and runs a simple loop that continues while the assistant makes tool
// calls.
package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/cognitive"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/provider/openai"
	"github.com/andrewhowdencom/ore/state"
	"github.com/andrewhowdencom/ore/tool"
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
	message := ""
	if len(os.Args) > 1 {
		// Join all arguments after the program name.
		for i, arg := range os.Args[1:] {
			if i > 0 {
				message += " "
			}
			message += arg
		}
	}
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
	apiKey := os.Getenv("ORE_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("ORE_API_KEY not set")
	}

	model := os.Getenv("ORE_MODEL")
	if model == "" {
		model = "gpt-4o"
	}

	baseURL := os.Getenv("ORE_BASE_URL")

	// Create tool registry with calculator functions.
	registry := tool.NewRegistry()
	registry.Register("add", func(ctx context.Context, args map[string]any) (any, error) {
		a := toFloat64(args["a"])
		b := toFloat64(args["b"])
		return a + b, nil
	})
	registry.Register("multiply", func(ctx context.Context, args map[string]any) (any, error) {
		a := toFloat64(args["a"])
		b := toFloat64(args["b"])
		return a * b, nil
	})

	// Define tools for the provider.
	tools := []provider.Tool{
		{
			Name:        "add",
			Description: "Add two numbers together",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"a": map[string]any{"type": "number", "description": "The first number"},
					"b": map[string]any{"type": "number", "description": "The second number"},
				},
				"required": []string{"a", "b"},
			},
		},
		{
			Name:        "multiply",
			Description: "Multiply two numbers together",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"a": map[string]any{"type": "number", "description": "The first number"},
					"b": map[string]any{"type": "number", "description": "The second number"},
				},
				"required": []string{"a", "b"},
			},
		},
	}

	// Build provider.
	var opts []openai.Option
	if baseURL != "" {
		opts = append(opts, openai.WithBaseURL(baseURL))
	}
	prov := openai.New(apiKey, model, opts...)

	// Build state with the user message.
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: message})

	// Create step with tool handler and pre-bound tool options.
	step := loop.New(
		loop.WithHandlers(registry.Handler()),
		loop.WithInvokeOptions(openai.WithTools(tools)),
	)

	// Run the cognitive pattern.
	react := &cognitive.ReAct{
		Step:     step,
		Provider: prov,
	}

	result, err := react.Run(ctx, mem)
	if err != nil {
		return fmt.Errorf("react failed: %w", err)
	}

	// Print assistant artifacts from the final response.
	turns := result.Turns()
	last := turns[len(turns)-1]
	for _, art := range last.Artifacts {
		switch a := art.(type) {
		case artifact.Text:
			fmt.Println(a.Content)
		case artifact.Reasoning:
			fmt.Printf("--- reasoning ---\n%s\n", a.Content)
		case artifact.ToolCall:
			fmt.Printf("--- tool_call: %s ---\n%s\n", a.Name, a.Arguments)
		case artifact.Usage:
			fmt.Printf("--- usage: %d prompt / %d completion / %d total ---\n",
				a.PromptTokens, a.CompletionTokens, a.TotalTokens)
		default:
			fmt.Printf("--- %s ---\n[unsupported artifact type]\n", art.Kind())
		}
	}

	return nil
}

// toFloat64 converts a JSON-decoded number (or string) to float64.
func toFloat64(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case string:
		f, _ := strconv.ParseFloat(n, 64)
		return f
	}
	return 0
}
