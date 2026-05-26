// Package session provides the Stream and Manager primitives that
// orchestrate per-session inference and session lifecycle in the ore
// framework.
//
// Stream is a per-session primitive that owns the loop.Step,
// thread.Thread, TurnProcessor, and provider for a single active
// conversation. It provides ingress (Submit, Process) and egress
// (Subscribe) plus lifecycle controls (Cancel, Close).
//
// Submit enqueues an event into the stream's internal FIFO queue and
// returns immediately. A single worker goroutine processes events one
// at a time in order, so concurrent submissions are naturally
// serialized without dropping messages.
//
// Process is a convenience wrapper around Submit that blocks until
// the worker has finished processing the enqueued event. It is useful
// when the caller needs to know when a turn is complete (e.g. HTTP
// handlers that must close an NDJSON response stream).
//
// Manager is a factory/registry for Stream handles. It creates and
// manages active streams, each pairing a persistent thread.Thread with
// an ephemeral loop.Step. Applications configure a Manager with a
// provider, step factory, and cognitive pattern (TurnProcessor).
// Conduits obtain a *Stream from the Manager (via Create, Attach, or
// Get) and invoke Submit, Process, Subscribe, Cancel, and Close on
// that handle, never touching loop.Step directly.
//
// Migration note: the Session interface has been removed. Use
// *session.Stream directly. Event types (Event, UserMessageEvent,
// InterruptEvent) have moved from the conduit package to session.
// All event types carry an optional loop.EventContext for routing
// metadata (e.g., provenance). Set it when constructing the event:
//
//	UserMessageEvent{Content: "hello", Ctx: loop.EventContext{Provenance: "slack-123"}}
//
// Lifecycle events:
//
//	ProcessCompleteEvent is emitted after the queue worker finishes
//	processing an event (including all tool-call loops). It carries the
//	final error state and is the preferred signal for UI-level lifecycle
//	actions (audio notifications, typing indicator dismissal). Conduits
//	should subscribe to it in addition to TurnCompleteEvent, which fires
//	on every intermediate turn for incremental rendering.
//
// To persist state across turns, wire an OnEmit callback that appends
// TurnCompleteEvent to the thread's state buffer. Typical composition:
//
//	store := thread.NewMemoryStore()
//	prov := openai.New(apiKey, model)
//	stepFactory := func(thr *thread.Thread) (*loop.Step, error) {
//		return loop.New(loop.WithOnEmit(func(ctx context.Context, event loop.OutputEvent) {
//			if tc, ok := event.(loop.TurnCompleteEvent); ok {
//				thr.State.Append(tc.Turn.Role, tc.Turn.Artifacts...)
//			}
//		})), nil
//	}
//	mgr := session.NewManager(store, prov, stepFactory, cognitive.NewTurnProcessor())
//
// The factory receives *thread.Thread so it can bind per-session state.
// For example, a factory can close over the thread to inject a dynamic
// system prompt that reads from thread.GetMetadata("persona"):
//
//	stepFactory := func(thr *thread.Thread) (*loop.Step, error) {
//		// Build transforms that use thr.Metadata, thr.ID, etc.
//		return loop.New(loop.WithOnEmit(func(ctx context.Context, event loop.OutputEvent) {
//			if tc, ok := event.(loop.TurnCompleteEvent); ok {
//				thr.State.Append(tc.Turn.Role, tc.Turn.Artifacts...)
//			}
//		})), nil
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
//	// Emit custom output events (e.g. status updates) into the stream's FanOut.
//	_ = stream.Emit(ctx, loop.StatusEvent{Status: map[string]string{"thread_id": stream.ID()}})
package session
