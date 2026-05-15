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
//
// Typical composition:
//
//	store := thread.NewMemoryStore()
//	prov := openai.New(apiKey, model)
//	stepFactory := func() *loop.Step { return loop.New() }
//	mgr := session.NewManager(store, prov, stepFactory, cognitive.NewTurnProcessor())
//
//	// Obtain a *Stream from the manager.
//	stream, _ := mgr.Create()
//
//	// Subscribe to output events via the Stream handle.
//	ch, _ := stream.Subscribe("text_delta", "turn_complete")
//
//	// Process an event via the Stream handle.
//	_ = stream.Process(ctx, UserMessageEvent{Content: "hello"})
//
//	// HTTP conduit composes with the Manager.
//	c, _ := httpc.New(mgr, httpc.WithUI(), httpc.WithAddr(":8080"))
//	_ = c.Start(ctx)
//
//	// TUI conduit composes with the Manager.
//	tuiConduit, _ := tui.New(mgr)
//	_ = tuiConduit.Start(ctx)
package session
