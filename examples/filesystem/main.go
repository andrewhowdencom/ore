// filesystem is a reference application demonstrating filesystem tool
// calling with ore using the agent.Agent primitive. It registers
// read_file, write_file, edit_file, list_directory, and search_files
// tools, configures an OpenAI provider with them, and runs a ReAct
// loop that continues while the assistant makes tool calls.
package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/andrewhowdencom/ore/agent"
	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/cognitive"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/state"
	"github.com/andrewhowdencom/ore/tool"
	"github.com/andrewhowdencom/ore/x/provider/openai"
	xtool "github.com/andrewhowdencom/ore/x/tool"
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

	// Build state with the user message.
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: message})

	// Construct the agent: provider, model spec, ReAct pattern, tool
	// handler, pre-bound tool options, and a bound state. The pattern
	// is configured empty — its Step, Provider, Spec, and tracer are
	// injected at agent construction time via SetRuntime. The bound
	// state's auto-append path replaces the explicit OnEmit callback
	// the old code used to copy turns into mem.
	a := agent.New("filesystem",
		agent.WithProvider(prov),
		agent.WithSpec(models.Spec{Name: model}),
		agent.WithPattern(&cognitive.ReAct{}),
		agent.WithHandlers(xtool.NewHandler(registry)),
		agent.WithInvokeOptions(openai.WithTools(registry.Tools())),
		agent.WithState(mem),
	)
	defer a.Close()

	result, err := a.Run(ctx, mem)
	if err != nil {
		return fmt.Errorf("agent run failed: %w", err)
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
