// Package junk holds session-orchestration primitives that are pending
// extraction into better-defined packages. The intent is to drive the
// surface area of this package to zero over time.
//
// Package session provides the Stream and Manager primitives that
// orchestrate per-session inference and session lifecycle in the ore
// framework.
//
// Stream is a per-session primitive that owns the loop.Step,
// Thread, TurnProcessor, and provider for a single active
// conversation. It provides ingress (Submit, Process) and egress
// (Subscribe) plus lifecycle controls (Cancel, Close).
//
// Submit enqueues an event into the stream's internal FIFO queue and
// returns immediately. A single worker goroutine processes events one
// at a time in order, so concurrent submissions are naturally
// serialized without dropping messages.
//
// The worker goroutine starts lazily on the first Submit or Process
// call via sync.Once, avoiding goroutine overhead for idle streams.
// Submit returns an error only if the stream has been closed; it never
// returns ErrSessionBusy (which has been removed).
//
// Process is a convenience wrapper around Submit that blocks until
// the worker has finished processing the enqueued event. It is useful
// when the caller needs to know when a turn is complete (e.g. HTTP
// handlers that must close an NDJSON response stream).
//
// Manager is a factory/registry for Stream handles. It creates and
// manages active streams, each pairing a persistent Thread with
// an ephemeral loop.Step. Applications configure a Manager with a
// provider, step factory, and cognitive pattern (TurnProcessor).
// Conduits obtain a *Stream from the Manager (via Create, Attach, or
// Get) and invoke Submit, Process, Subscribe, Cancel, and Close on
// that handle, never touching loop.Step directly.
//
// Migration note: the Session interface has been removed. Use
// *junk.Stream directly. Event types (Event, UserMessageEvent,
// InterruptEvent) have moved from the conduit package to junk.
// All event types carry an optional context.Context for routing
// metadata (e.g., provenance). Set it when constructing the event:
//
//	UserMessageEvent{Content: "hello", Ctx: loop.WithProvenance(context.Background(), "slack-123")}
//
// Lifecycle events:
//
//	LifecycleEvent signals phase transitions for the inference pipeline:
//	"submitted" (message accepted), "streaming" (first artifact arrived),
//	and "done" (turn or pipeline complete). Conduits should subscribe to
//	it to drive UI state without inferring lifecycle from data events.
//
// State persistence is handled automatically by the Manager via
// loop.WithState, which binds the thread's state to the Step so that
// TurnCompleteEvent is appended after every turn. Applications only
// need a stepFactory when adding custom transforms, handlers, or other
// loop.Step options:
//
//	store := NewMemoryStore()
//	prov, _ := openai.New(openai.WithAPIKey(apiKey), openai.WithModel(model))
//	stepFactory := func(stream *Stream) ([]loop.Option, error) {
//		// Build transforms that use stream.Metadata, stream.ID, etc.
//		return nil, nil  // use default step with auto-persistence
//	}
//	mgr := junk.NewManager(store, prov, stepFactory, cognitive.NewTurnProcessor(cognitive.ReActFactory, tracer))
//
// The factory receives *Stream so it can bind per-session runtime ledger.
// For example, a factory can close over the stream to inject a dynamic
// system prompt that reads from stream.GetMetadata("persona"):
//
//	stepFactory := func(stream *Stream) ([]loop.Option, error) {
//		sp, _ := systemprompt.New(systemprompt.WithContentFunc(func() string {
//			if p, ok := stream.GetMetadata("persona"); ok {
//				return "You are a " + p + "."
//			}
//			return "You are a helpful assistant."
//		}))
//		return []loop.Option{loop.WithTransforms(sp)}, nil
//	}
//
//	// Obtain a *Stream from the manager.
//	stream, _ := mgr.Create()
//
//	// Subscribe to output events via the Stream handle.
//	ch := stream.Subscribe("text_delta", "turn_complete")
//
//	// Submit an event via the Stream handle (non-blocking).
//	_ = stream.Submit(UserMessageEvent{Content: "hello"})
//
//	// HTTP conduit composes with the Manager. UI is enabled by default.
//	c, _ := httpc.New(mgr, httpc.WithAddr(":8080"))
//	_ = c.Start(ctx)
//
//	// TUI conduit composes with the Manager.
//	tuiConduit, _ := tui.New(mgr)
//	_ = tuiConduit.Start(ctx)
//
//	// Retrieve the conversation history without exposing the internal Thread.
//	turns := stream.Turns()
//
//	// Emit custom output events (e.g. status updates) into the stream's FanOut.
//	_ = stream.Emit(ctx, loop.PropertiesEvent{Properties: map[string]string{"thread_id": stream.ID()}})
package junk
