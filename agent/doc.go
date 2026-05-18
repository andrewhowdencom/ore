// Package agent provides multi-conduit orchestration for ore agent
// applications. An Agent coordinates multiple conduit.Conduit instances,
// running them concurrently with a shared session.Manager.
//
// Use New to create an Agent, Add to register conduits, and Run to start
// all registered conduits. Run blocks until the context is cancelled or
// any conduit returns a non-nil error.
package agent
