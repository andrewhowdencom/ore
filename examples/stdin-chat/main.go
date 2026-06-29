// stdin-chat is a reference application demonstrating a multi-turn
// chat loop using the ore agent.Agent primitive. The agent is
// constructed once and reused across many Run calls; the bound
// state accumulates the conversation across turns.
//
// Usage:
//
//	ORE_API_KEY=... go run ./examples/stdin-chat
//
// Type a message and press enter to receive a reply. Empty line,
// "exit", "quit", or EOF terminates the loop.
package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/andrewhowdencom/ore/agent"
	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/cognitive"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/ledger"
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
	ctx := context.Background()

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

	// Build provider.
	var opts []openai.Option
	if baseURL != "" {
		opts = append(opts, openai.WithBaseURL(baseURL))
	}
	prov, err := openai.New(append([]openai.Option{openai.WithAPIKey(apiKey)}, opts...)...)
	if err != nil {
		return fmt.Errorf("create openai provider: %w", err)
	}

	// Construct the agent: SingleShot pattern (one inference call per
	// user message; no tools registered), bound ledger. The agent is
	// reused across many Run calls; the bound state accumulates the
	// conversation across turns. The bound state's auto-append path
	// emits the assistant's turn into mem after each Run; the user
	// turn is appended manually before each call.
	mem := &ledger.Buffer{}
	a := agent.New("stdin-chat",
		agent.WithProvider(prov),
		agent.WithSpec(models.Spec{Name: modelName}),
		agent.WithPattern(&cognitive.SingleShot{}),
		agent.WithState(mem),
	)
	defer a.Close()

	slog.Info("stdin-chat: type a message, then enter. Empty line, 'exit', 'quit', or EOF to quit.")

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Fprint(os.Stderr, "> ")
		if !scanner.Scan() {
			// EOF or read error.
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("read stdin: %w", err)
			}
			return nil
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line == "exit" || line == "quit" {
			return nil
		}

		// Append the user turn, run the agent, print the assistant's reply.
		// The agent's internal step serializes mem (now containing the user
		// turn) to the provider, then auto-appends the assistant turn back
		// into mem via WithState.
		mem.Append(ledger.RoleUser, artifact.Text{Content: line})
		result, err := a.Run(ctx, mem)
		if err != nil {
			return fmt.Errorf("agent run failed: %w", err)
		}

		// Print the latest assistant turn's text artifacts. Other
		// artifact kinds (reasoning, tool calls, etc.) are skipped
		// for text-only output.
		turns := result.Turns()
		if len(turns) == 0 {
			continue
		}
		last := turns[len(turns)-1]
		if last.Role != ledger.RoleAssistant {
			continue
		}
		for _, art := range last.Artifacts {
			if t, ok := art.(artifact.Text); ok {
				fmt.Println(t.Content)
			}
		}
	}
}
