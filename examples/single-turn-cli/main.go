// single-turn-cli is a reference application demonstrating composition of the
// ore loop.Step with an OpenAI-compatible provider adapter.
//
// This example shows both inline transform construction and the extension
// module pattern for injecting system prompts into the inference context.
package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/provider/openai"
	"github.com/andrewhowdencom/ore/state"
)

// systemPromptTransform is an inline loop.Transform that prepends a
// RoleSystem turn to the inference context without mutating the
// persistent conversation buffer. It demonstrates the transform
// interface for ad-hoc, application-specific state assembly.
type systemPromptTransform struct{ content string }

func (t *systemPromptTransform) Transform(ctx context.Context, st state.State) (state.State, error) {
	return state.NewVirtualTurnState(st, []state.Turn{{
		Role:      state.RoleSystem,
		Artifacts: []artifact.Artifact{artifact.Text{Content: t.content}},
	}}), nil
}

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
	apiKey := os.Getenv("ORE_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("ORE_API_KEY not set")
	}

	model := os.Getenv("ORE_MODEL")
	if model == "" {
		model = "gpt-4o"
	}

	baseURL := os.Getenv("ORE_BASE_URL")

	// Build state with the user message.
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: message})

	// Build provider.
	var opts []openai.Option
	if baseURL != "" {
		opts = append(opts, openai.WithBaseURL(baseURL))
	}
	p := openai.New(apiKey, model, opts...)

	// Tool calling example (uncomment this block and comment out the provider
	// and step setup immediately above and below it):
	//
	//   registry := tool.NewRegistry()
	//   registry.Register("calculator", "A simple calculator", map[string]any{"type": "object"}, func(ctx context.Context, args map[string]any) (any, error) {
	//       return "42", nil
	//   })
	//   p := openai.New(apiKey, model, opts...)
	//   s := loop.New(loop.WithHandlers(registry.Handler()), loop.WithInvokeOptions(openai.WithTools(registry.Tools())))
	//
	// Note: to use tools, loop until the assistant responds with text rather
	// than a single turn. See examples/calculator for a complete example.

	// Build the step. If ORE_SYSTEM_PROMPT is set, wire an inline
	// transform that injects it as a virtual RoleSystem turn. For
	// reusable transforms, use the extension module pattern:
	//
	//   import "github.com/andrewhowdencom/ore/x/systemprompt"
	//   sp, _ := systemprompt.New(systemprompt.WithContent("You are a helpful assistant."))
	//   s := loop.New(loop.WithTransforms(sp))
	var stepOpts []loop.Option
	if sysPrompt := os.Getenv("ORE_SYSTEM_PROMPT"); sysPrompt != "" {
		stepOpts = append(stepOpts, loop.WithTransforms(&systemPromptTransform{content: sysPrompt}))
	}
	s := loop.New(stepOpts...)

	// Execute a single loop turn.
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
