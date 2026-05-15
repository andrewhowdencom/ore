// Package main provides a reference HTTP-chat application demonstrating the
// ore HTTP conduit. It exposes a stateful chat server over HTTP with NDJSON
// streaming and an optional SSE ambient channel, backed by an OpenAI-compatible
// provider.
//
// A built-in web chat UI is served at http://localhost:8080/ when the
// application starts. Open a browser to interact without curl.
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
// Attach to an existing thread:
//
//	curl -s -X POST http://localhost:8080/sessions \
//	  -d '{"thread_id": "<uuid>"}' | jq -r '.id'
//
// List all threads:
//
//	curl -s http://localhost:8080/threads | jq '.'
//
// Delete the session:
//
//	curl -X DELETE http://localhost:8080/sessions/$SESSION_ID
//
// With persistent JSON store:
//
//	STORE_DIR=/tmp/ore-store go run ./examples/http-chat
//
// The server optionally registers calculator tools (add, multiply) to
// demonstrate server-side ReAct loop execution. See package tool for details
// on the registry.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"

	"github.com/andrewhowdencom/ore/cognitive"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/provider/openai"
	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/thread"
	"github.com/andrewhowdencom/ore/tool"

	httpc "github.com/andrewhowdencom/ore/x/conduit/http"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		slog.Error("fatal error", "err", err)
		os.Exit(1)
	}
}

// run parses configuration, builds the provider and tool registry, and starts
// the HTTP server.
func run() error {
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

	// Create the thread store.
	var threadStore thread.Store
	if storeDir := os.Getenv("STORE_DIR"); storeDir != "" {
		var err error
		threadStore, err = thread.NewJSONStore(storeDir)
		if err != nil {
			return fmt.Errorf("create JSON store: %w", err)
		}
	} else {
		threadStore = thread.NewMemoryStore()
	}

	// Create the session manager with the ReAct cognitive pattern.
	mgr := session.NewManager(threadStore, prov, stepFactory, cognitive.NewTurnProcessor())

	// Create the HTTP conduit handler.
	// WithUI() is optional; omit it to serve only the API without the chat UI.
	handler := httpc.NewHandler(mgr, httpc.WithUI())

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
