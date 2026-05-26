// Package session provides the Stream and Manager primitives that
// orchestrate per-session inference and session lifecycle in the ore
// framework.
//
// Stream is a per-session primitive that owns the loop.Step,
// thread.Thread, TurnProcessor, and provider for a single active
// conversation. It provides ingress (Process) and egress (Subscribe)
// plus lifecycle controls (Cancel, Close).
//
// Manager is a factory/registry for Stream handles. It creates and
// manages active streams, each pairing a persistent thread.Thread with
// an ephemeral loop.Step. Applications configure a Manager with a
// provider, step factory, and cognitive pattern (TurnProcessor).
// Conduits obtain a *Stream from the Manager (via Create, Attach, or
// Get) and invoke Process, Subscribe, Cancel, and Close on that
// handle, never touching loop.Step directly.
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
//	ProcessCompleteEvent is emitted after Process() finishes the entire
//	inference pipeline (including all tool-call loops). It carries the
//	final error state and is the preferred signal for UI-level lifecycle
//	actions (audio notifications, typing indicator dismissal). Conduits
//	should subscribe to it in addition to TurnCompleteEvent, which fires
//	on every intermediate turn for incremental rendering.
//
// Typical composition:
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
//	// Process an event via the Stream handle.
//	_ = stream.Process(ctx, UserMessageEvent{Content: "hello"})
//
//	// HTTP conduit composes with the Manager. UI is enabled by default.
//	c, _ := httpc.New(mgr, httpc.WithAddr(":8080"))
//	_ = c.Start(ctx)
//
//	// TUI conduit composes with the Manager.
//	tuiConduit, _ := tui.New(mgr)
//	_ = tuiConduit.Start(ctx)
package session
