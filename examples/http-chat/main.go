// Package main provides a reference HTTP-chat application demonstrating the
// ore HTTP conduit. It exposes a stateful chat server over HTTP with NDJSON
// streaming and an optional SSE ambient channel, backed by an OpenAI-compatible
// provider.
//
// A built-in web chat UI is served at http://localhost:8080/chat when the
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
// demonstrate server-side ReAct loop execution. The core registry contract
// lives in package tool; the handler bridge lives in package x/tool.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"

	"github.com/andrewhowdencom/ore/cognitive"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/tool"
	"github.com/andrewhowdencom/ore/x/provider/openai"
	"github.com/andrewhowdencom/ore/x/slash"
	xtool "github.com/andrewhowdencom/ore/x/tool"
	"github.com/andrewhowdencom/ore/x/tool/calculator"
	"github.com/andrewhowdencom/ore/x/telemetry"
	"github.com/andrewhowdencom/ore/x/usage"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	httpc "github.com/andrewhowdencom/ore/x/conduit/http"
	"go.opentelemetry.io/otel/trace/noop"
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

	// Create a noop tracer for the example (replace with a real OTel setup in
	// production). This demonstrates how tracing is wired through all components.
	tracer := noop.NewTracerProvider().Tracer("http-chat")

	// Application version for OTel resource attributes.
	version := os.Getenv("APP_VERSION")
	if version == "" {
		version = "dev"
	}

	// Create a real meter provider with a version resource attribute.
	res, err := sdkresource.New(context.Background(),
		sdkresource.WithAttributes(attribute.String("service.version", version)),
	)
	if err != nil {
		return fmt.Errorf("create OTel resource: %w", err)
	}

	mp := sdkmetric.NewMeterProvider(sdkmetric.WithResource(res))
	defer mp.Shutdown(context.Background())

	meter := mp.Meter("http-chat")
	tel := telemetry.New(meter)

	// Build OpenAI provider.
	var opts []openai.Option
	if baseURL != "" {
		opts = append(opts, openai.WithBaseURL(baseURL))
	}
	prov, err := openai.New(append([]openai.Option{
		openai.WithAPIKey(apiKey),
		openai.WithModel(modelName),
		openai.WithTracer(tracer),
	}, opts...)...)
	if err != nil {
		return fmt.Errorf("create openai provider: %w", err)
	}

	// Create a tool registry with calculator functions.
	// These are optional — remove them for a simple chat server.
	registry := tool.NewRegistry()
	if err := registry.Register(calculator.AddTool, calculator.Add); err != nil {
		return fmt.Errorf("register add tool: %w", err)
	}
	if err := registry.Register(calculator.MultiplyTool, calculator.Multiply); err != nil {
		return fmt.Errorf("register multiply tool: %w", err)
	}

	// Step factory: each session gets its own Step with tool handler,
	// usage handler, and provider tool options bound. State persistence
	// is handled automatically by Manager; no custom OnEmit needed.
	stepFactory := func(stream *session.Stream) ([]loop.Option, error) {
		return []loop.Option{
			loop.WithHandlers(xtool.NewHandler(registry, xtool.WithTracer(tracer)), usage.New()),
			loop.WithOnEmit(tel.OnEmit()),
			loop.WithInvokeOptions(openai.WithTools(registry.Tools())),
			loop.WithTracer(tracer),
		}, nil
	}

	// Create the thread store.
	var threadStore session.Store
	if storeDir := os.Getenv("STORE_DIR"); storeDir != "" {
		var err error
		threadStore, err = session.NewJSONStore(storeDir)
		if err != nil {
			return fmt.Errorf("create JSON store: %w", err)
		}
	} else {
		threadStore = session.NewMemoryStore()
	}

	// Create a slash command registry. Commands are intercepted before they
	// reach the LLM inference pipeline, allowing meta-operations like session
	// switching without triggering a model turn.
	slashReg := slash.NewRegistry()

	// Create the session manager with the ReAct cognitive pattern.
	// Wire the slash command registry as an interceptor so user messages
	// starting with "/" are handled by slash commands before inference.
	mgr := session.NewManager(
		threadStore,
		prov,
		stepFactory,
		cognitive.NewTurnProcessor(cognitive.ReActFactory, tracer),
		session.WithInterceptor(slashReg),
	)

	// Bind slash commands after the manager is created so handlers can
	// capture the manager in their closures.
	slashReg.Bind("new", "Create a new session", func(ctx context.Context, cmd slash.Command) (slash.Result, error) {
		// Create a new session and emit a SessionSwitchEvent to notify
		// all conduits subscribed to the current stream that the user
		// wants to navigate to a new session.
		stream, err := mgr.Create()
		if err != nil {
			return slash.Result{}, fmt.Errorf("create session: %w", err)
		}
		slog.Info("slash command: /new", "new_session", stream.ID())
		return slash.Result{Replace: session.SessionSwitchEvent{
			SessionID: stream.ID(),
			Ctx:       loop.WithProvenance(ctx, "slash"),
		}}, nil
	})

	slashReg.Bind("compact", "Compact conversation history", func(ctx context.Context, cmd slash.Command) (slash.Result, error) {
		slog.Info("slash command: /compact", "args", slash.Fields(cmd.Input))
		return slash.Result{}, nil
	})

	// Create the HTTP conduit.
	// UI is enabled by default in New(); use httpc.WithoutUI() to disable it.
	c, err := httpc.New(mgr, httpc.WithUI(), httpc.WithAddr(":"+port), httpc.WithTracer(tracer))
	if err != nil {
		return fmt.Errorf("create HTTP conduit: %w", err)
	}

	// Start the server and block until interrupted.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	slog.Info("starting HTTP server", "addr", ":"+port)
	return c.Start(ctx)
}