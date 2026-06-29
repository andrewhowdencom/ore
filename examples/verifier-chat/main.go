// verifier-chat is a reference application demonstrating the WithVerification
// cognitive pattern. The agent receives a coding task, writes Go code using
// filesystem tools, and must pass quality gates (go build, go test, gofmt)
// before returning. If any gate fails, the combined report is injected as a
// system turn and the agent retries.
//
// Usage:
//
//	export ORE_API_KEY=...
//	export ORE_MODEL=gpt-4o
//	go run ./examples/verifier-chat "Write a function that reverses a string"
package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/cognitive"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/andrewhowdencom/ore/tool"
	"github.com/andrewhowdencom/ore/x/provider/openai"
	xtool "github.com/andrewhowdencom/ore/x/tool"
	"github.com/andrewhowdencom/ore/x/tool/filesystem"
	"github.com/andrewhowdencom/ore/x/verifier"
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

	// Create a temporary directory for the agent to work in.
	tempDir, err := os.MkdirTemp("", "ore-verifier-chat-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Change to the temp directory so the agent writes files there
	// and verifiers run in the correct location.
	origDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	if err := os.Chdir(tempDir); err != nil {
		return fmt.Errorf("chdir temp dir: %w", err)
	}
	defer os.Chdir(origDir)

	// Create tool registry with filesystem functions.
	registry := tool.NewRegistry()
	if err := registry.Register(filesystem.ReadFileTool, filesystem.ReadFile); err != nil {
		return fmt.Errorf("register read_file tool: %w", err)
	}
	if err := registry.Register(filesystem.WriteFileTool, filesystem.WriteFile); err != nil {
		return fmt.Errorf("register write_file tool: %w", err)
	}
	if err := registry.Register(filesystem.EditFileTool, filesystem.EditFile); err != nil {
		return fmt.Errorf("register edit_file tool: %w", err)
	}
	if err := registry.Register(filesystem.ListDirectoryTool, filesystem.ListDirectory); err != nil {
		return fmt.Errorf("register list_directory tool: %w", err)
	}
	if err := registry.Register(filesystem.SearchFilesTool, filesystem.SearchFiles); err != nil {
		return fmt.Errorf("register search_files tool: %w", err)
	}

	// Build provider.
	var opts []openai.Option
	if baseURL != "" {
		opts = append(opts, openai.WithBaseURL(baseURL))
	}
	prov, err := openai.New(append([]openai.Option{
		openai.WithAPIKey(apiKey),
		
	}, opts...)...)
	if err != nil {
		return fmt.Errorf("create openai provider: %w", err)
	}

	// Build state with a system prompt and the user message.
	mem := &ledger.Buffer{}
	mem.Append(ledger.RoleSystem, artifact.Text{Content: fmt.Sprintf(
		"You are a Go coding agent. Write Go code to files in the current directory (%s). "+
		"Make sure your code compiles with `go build ./...`, passes tests with `go test ./...`, "+
		"and is formatted with `gofmt -d .`.",
		tempDir,
	)})
	mem.Append(ledger.RoleUser, artifact.Text{Content: message})

	// Create step with tool handler and pre-bound tool options.
	step := loop.New(
		loop.WithHandlers(xtool.NewHandler(registry)),
		loop.WithInvokeOptions(openai.WithTools(registry.Tools())),
		loop.WithOnEmit(func(ctx context.Context, event loop.OutputEvent) {
			if tc, ok := event.(loop.TurnCompleteEvent); ok {
				mem.Append(tc.Turn.Role, tc.Turn.Artifacts...)
			}
		}),
	)

	// Create quality gate verifiers.
	verifiers := []verifier.Verifier{
		&verifier.ExecVerifier{
			Name:    "go build",
			Command: "go",
			Args:    []string{"build", "./..."},
		},
		&verifier.ExecVerifier{
			Name:    "go test",
			Command: "go",
			Args:    []string{"test", "./..."},
		},
		&verifier.ExecVerifier{
			Name:    "gofmt",
			Command: "gofmt",
			Args:    []string{"-d", "."},
		},
	}

	// Run the cognitive pattern with verification.
	react := &cognitive.ReAct{
		Step:     step,
		Provider: prov,
	}
	verified := cognitive.WithVerification(
		react,
		step,
		cognitive.WithVerifiers(verifiers...),
		cognitive.WithMaxRetries(3),
	)

	result, err := verified.Run(ctx, mem)
	if err != nil {
		return fmt.Errorf("verified react failed: %w", err)
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
