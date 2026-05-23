// filesystem is a reference application demonstrating filesystem tool calling
// with ore. It registers read_file, write_file, edit_file, list_directory and
// search_files tools, configures an OpenAI provider with them, and runs a
// simple loop that continues while the assistant makes tool calls.
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
	"github.com/andrewhowdencom/ore/x/provider/openai"
	"github.com/andrewhowdencom/ore/state"
	"github.com/andrewhowdencom/ore/x/tool"
	"github.com/andrewhowdencom/ore/x/tool/filesystem"
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

	// Create tool registry with filesystem functions.
	registry := tool.NewRegistry()
	if err := registry.Register(filesystem.ReadFileTool.Name, filesystem.ReadFileTool.Description, filesystem.ReadFileTool.Schema, filesystem.ReadFile); err != nil {
		return fmt.Errorf("register read_file tool: %w", err)
	}
	if err := registry.Register(filesystem.WriteFileTool.Name, filesystem.WriteFileTool.Description, filesystem.WriteFileTool.Schema, filesystem.WriteFile); err != nil {
		return fmt.Errorf("register write_file tool: %w", err)
	}
	if err := registry.Register(filesystem.EditFileTool.Name, filesystem.EditFileTool.Description, filesystem.EditFileTool.Schema, filesystem.EditFile); err != nil {
		return fmt.Errorf("register edit_file tool: %w", err)
	}
	if err := registry.Register(filesystem.ListDirectoryTool.Name, filesystem.ListDirectoryTool.Description, filesystem.ListDirectoryTool.Schema, filesystem.ListDirectory); err != nil {
		return fmt.Errorf("register list_directory tool: %w", err)
	}
	if err := registry.Register(filesystem.SearchFilesTool.Name, filesystem.SearchFilesTool.Description, filesystem.SearchFilesTool.Schema, filesystem.SearchFiles); err != nil {
		return fmt.Errorf("register search_files tool: %w", err)
	}

	// Build provider.
	var opts []openai.Option
	if baseURL != "" {
		opts = append(opts, openai.WithBaseURL(baseURL))
	}
	prov, err := openai.New(append([]openai.Option{
		openai.WithAPIKey(apiKey),
		openai.WithModel(model),
	}, opts...)...)
	if err != nil {
		return fmt.Errorf("create openai provider: %w", err)
	}

	// Build state with the user message.
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: message})

	// Create step with tool handler and pre-bound tool options.
	step := loop.New(
		loop.WithHandlers(registry.Handler()),
		loop.WithInvokeOptions(openai.WithTools(registry.Tools())),
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
