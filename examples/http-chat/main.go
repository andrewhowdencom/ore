// http-chat is a reference application demonstrating the ore HTTP conduit.
// It exposes a stateful chat server over HTTP with NDJSON streaming and
// an optional SSE ambient channel, backed by an OpenAI-compatible provider.
//
// Usage:
//
//	export ORE_API_KEY=...
//	export ORE_MODEL=gpt-4o
//	go run ./examples/http-chat
//
// Create a session and capture the ID:
//
//	SESSION_ID=$(curl -s -X POST http://localhost:8080/sessions | jq -r '.id')
//
// Send a message (stream NDJSON):
//
//	curl -N -X POST http://localhost:8080/sessions/$SESSION_ID/messages \
//	  -H "Content-Type: application/json" \
//	  -d '{"content": "What is 2 + 3?"}'
//
// Subscribe to SSE events (using the events_url from creation):
//
//	curl -N http://localhost:8080/sessions/$SESSION_ID/events?kinds=text_delta,turn_complete
//
// Delete the session:
//
//	curl -X DELETE http://localhost:8080/sessions/$SESSION_ID
//
// The server optionally registers calculator tools (add, multiply) to
// demonstrate server-side ReAct loop execution.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/cognitive"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/provider/openai"
	"github.com/andrewhowdencom/ore/state"
	"github.com/andrewhowdencom/ore/tool"

	httpc "github.com/andrewhowdencom/ore/conduit/http"
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
	_ = ctx // used by tool functions

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
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Build OpenAI provider.
	var opts []openai.Option
	if baseURL != "" {
		opts = append(opts, openai.WithBaseURL(baseURL))
	}
	prov := openai.New(apiKey, modelName, opts...)

	// Create a tool registry with calculator functions.
	// These are optional — remove them for a simple chat server.
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

	// Step factory: each session gets its own Step with tool handler
	// and provider tool options bound.
	stepFactory := func() *loop.Step {
		return loop.New(
			loop.WithHandlers(registry.Handler()),
			loop.WithInvokeOptions(openai.WithTools(tools)),
		)
	}

	// Message handler: the application composes the ReAct cognitive pattern
	// just like the TUI example does. The library handles HTTP and streaming;
	// the application decides what to do with each message.
	messageHandler := func(ctx context.Context, session *httpc.Session, content string) error {
		if _, err := session.Step().Submit(ctx, session.State(), state.RoleUser, artifact.Text{Content: content}); err != nil {
			return err
		}
		react := &cognitive.ReAct{
			Step:     session.Step(),
			Provider: prov,
		}
		_, err := react.Run(ctx, session.State())
		return err
	}

	// Create the HTTP conduit handler.
	handler := httpc.NewHandler(stepFactory, messageHandler)

	// Start the HTTP server.
	server := &http.Server{
		Addr:    ":" + port,
		Handler: handler.ServeMux(),
	}

	slog.Info("starting HTTP server", "addr", server.Addr)
	return server.ListenAndServe()
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
