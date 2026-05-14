// Package session provides a Manager primitive that owns the Thread↔Step
// binding and the inference pipeline for the ore framework.
//
// The Manager creates and manages active sessions, each pairing a
// persistent thread.Thread with an ephemeral loop.Step. Applications
// configure a Manager with a provider, step factory, and cognitive
// pattern (TurnProcessor). Conduits attach to the Manager to feed
// ingress events and consume output streams, never touching loop.Step
// directly.
//
// Usage:
//
//	mgr := session.NewManager(store, prov, stepFactory, cognitive.NewTurnProcessor())
//	sess, _ := mgr.Create()
//	ch, _ := mgr.Subscribe(sess.ID(), "text_delta", "turn_complete")
//	_ = mgr.Process(ctx, sess.ID(), conduit.UserMessageEvent{Content: "hello"})
//
package session
