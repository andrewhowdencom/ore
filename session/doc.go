// Package session provides a Manager primitive that owns the Thread↔Step
// binding and acts as a factory/registry for Session handles in the ore
// framework.
//
// The Manager creates and manages active sessions, each pairing a
// persistent thread.Thread with an ephemeral loop.Step. Applications
// configure a Manager with a provider, step factory, and cognitive
// pattern (TurnProcessor). Conduits obtain a Session from the Manager
// (via Create, Attach, or Get) and invoke Process, Subscribe, Cancel,
// and Close on that handle, never touching loop.Step directly.
//
// Typical composition:
//
//	store := thread.NewMemoryStore()
//	prov := openai.New(apiKey, model)
//	stepFactory := func() *loop.Step { return loop.New() }
//	mgr := session.NewManager(store, prov, stepFactory, cognitive.NewTurnProcessor())
//
//	// Obtain a Session handle from the manager.
//	sess, _ := mgr.Create()
//
//	// Subscribe to output events via the Session handle.
//	ch, _ := sess.Subscribe("text_delta", "turn_complete")
//
//	// Process an event via the Session handle.
//	_ = sess.Process(ctx, conduit.UserMessageEvent{Content: "hello"})
//
//	// HTTP conduit composes with the Manager.
//	handler := httpc.NewHandler(mgr, httpc.WithUI())
//
//	// TUI conduit composes with a Session handle.
//	t := tui.New(sess)
//
package session
