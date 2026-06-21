// Package main provides an interactive chat REPL demonstrating the ore
// framework. It wires together the ReAct cognitive pattern, the loop.Step
// primitive for turn orchestration, and the conduit/tui package for
// terminal interaction.
//
// Usage:
//
//	go run ./examples/tui-chat
//
// Resume an existing thread:
//
//	go run ./examples/tui-chat --thread <uuid>
//
// With persistent JSON store:
//
//	STORE_DIR=/tmp/ore-store go run ./examples/tui-chat
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/andrewhowdencom/ore/cognitive"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/x/analytics"
	"github.com/andrewhowdencom/ore/x/conduit/tui"
	"github.com/andrewhowdencom/ore/x/provider/openai"
	"github.com/andrewhowdencom/ore/x/slash"
	"github.com/andrewhowdencom/ore/x/telemetry"
	"github.com/andrewhowdencom/ore/x/tool/set_model"
	"github.com/andrewhowdencom/ore/x/tool/set_title"
	"github.com/andrewhowdencom/ore/x/usage"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/trace/noop"
)

func main() {
	buf := tui.NewLogBuffer()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, nil)))

	err := run()

	if flushErr := buf.FlushTo(os.Stderr); flushErr != nil {
		fmt.Fprintf(os.Stderr, "flush log buffer: %v\n", flushErr)
	}

	if err != nil {
		slog.Error("fatal error", "err", err)
		os.Exit(1)
	}
}

func run() error {
	// Parse command-line flags.
	var threadID string
	var listThreads bool
	flag.StringVar(&threadID, "thread", "", "existing thread UUID to resume")
	flag.BoolVar(&listThreads, "list-threads", false, "list all persisted threads and exit")
	flag.Parse()

	if listThreads {
		// Create thread store (respecting STORE_DIR env var).
		var store session.Store
		if storeDir := os.Getenv("STORE_DIR"); storeDir != "" {
			var err error
			store, err = session.NewJSONStore(storeDir)
			if err != nil {
				return fmt.Errorf("create JSON store: %w", err)
			}
		} else {
			store = session.NewMemoryStore()
		}

		threads, err := store.List()
		if err != nil {
			return fmt.Errorf("list threads: %w", err)
		}

		// Sort by UpdatedAt descending (most recently active first).
		sort.Slice(threads, func(i, j int) bool {
			return threads[i].UpdatedAt.After(threads[j].UpdatedAt)
		})

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tCreatedAt\tUpdatedAt")
		for _, t := range threads {
			fmt.Fprintf(w, "%s\t%s\t%s\n",
				t.ID,
				t.CreatedAt.Format(time.RFC3339),
				t.UpdatedAt.Format(time.RFC3339),
			)
		}
		w.Flush()

		return nil
	}

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

	// Create thread store.
	var store session.Store
	if storeDir := os.Getenv("STORE_DIR"); storeDir != "" {
		var err error
		store, err = session.NewJSONStore(storeDir)
		if err != nil {
			return fmt.Errorf("create JSON store: %w", err)
		}
	} else {
		store = session.NewMemoryStore()
	}

	// Create a noop tracer for the example (replace with a real OTel setup in
	// production). This demonstrates how tracing is wired through all components.
	tracer := noop.NewTracerProvider().Tracer("tui-chat")

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
	defer func() {
		if err := mp.Shutdown(context.Background()); err != nil {
			slog.Warn("shutdown meter provider", "err", err)
		}
	}()

	meter := mp.Meter("tui-chat")
	tel := telemetry.New(meter)

	// Build OpenAI provider.
	var opts []openai.Option
	if baseURL != "" {
		opts = append(opts, openai.WithBaseURL(baseURL))
	}
	prov, err := openai.New(append([]openai.Option{
		openai.WithAPIKey(apiKey),
		
		openai.WithTracer(tracer),
	}, opts...)...)
	if err != nil {
		return fmt.Errorf("create openai provider: %w", err)
	}

	// Build the slash command registry and bind the /name command for setting
	// the conversation title. The slash handler emits a PropertiesEvent
	// directly so the TUI's status zones (and window title) light up without
	// going through the tool/LLM pipeline.
	slashReg := slash.NewRegistry()
	slashReg.Bind("name", "Set the conversation title", set_title.Slash())
	slashReg.Bind("model", "Set the model for this session", set_model.Slash())
	slashReg.Bind("analytics", "Show per-(kind, source) byte and count breakdown for this thread",
		func(ctx context.Context, emitter loop.Emitter, cmd slash.Command) (slash.Result, error) {
			// Read-only handler: never invokes the LLM, never mutates state.
			// /analytics is slash-only by design — the model must not be
			// able to spend context budget calling it.
			//
			// The session.Stream is nil when the slash registry is invoked
			// outside the session pipeline (e.g. direct unit tests). Treat
			// that the same as an empty thread so the handler never panics
			// regardless of how it is wired.
			_ = emitter
			stream := cmd.Stream()
			if stream == nil {
				return slash.Result{
					Notice: loop.Notice{
						Content:  "No artifacts in this thread yet.",
						Severity: loop.SeverityInfo,
					},
				}, nil
			}
			stats := analytics.AnalyzeTurns(stream.Turns())
			return slash.Result{
				Notice: loop.Notice{
					Content:  analytics.Render(stats),
					Severity: loop.SeverityInfo,
				},
			}, nil
		},
	)

	// Manager now auto-persists state via a default OnEmit callback;
	// no custom stepFactory needed for basic TUI usage, but we wire the
	// usage handler so token counts are broadcast via PropertiesEvent.
	mgr := session.NewManager(store, prov, func(_ *session.Stream) ([]loop.Option, error) {
		return []loop.Option{
			loop.WithHandlers(usage.New()),
			loop.WithOnEmit(tel.OnEmit()),
			loop.WithTracer(tracer),
		}, nil
	}, cognitive.NewTurnProcessor(cognitive.ReActFactory, tracer),
		session.WithInterceptor(slashReg),
		// Seed the initial model name on new threads so the first
		// turn uses ORE_MODEL before any /model slash command runs.
		session.WithDefaultMetadata(func(*session.Stream) map[string]string {
			return map[string]string{
				session.MetadataKeyModelName: modelName,
			}
		}),
	)

	// Create the TUI conduit, passing the thread ID via functional option.
	// The TUI creates or attaches to the session internally on Start.
	c, err := tui.New(mgr,
		tui.WithName("tui-chat"),
		tui.WithThreadID(threadID),
		tui.WithTracer(tracer),
		tui.WithStatusZones(map[string]string{
			"phase":     "lifecycle",
			"title":     "lifecycle",
			"thread_id": "context",
			"model":     "context",
		}),
	)
	if err != nil {
		return fmt.Errorf("create TUI conduit: %w", err)
	}

	// Start the TUI and block until the user quits (Ctrl+C) or the
	// context is cancelled.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return c.Start(ctx)
}
