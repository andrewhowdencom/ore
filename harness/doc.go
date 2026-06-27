// Package harness provides multi-conduit orchestration for ore agent
// applications. A Host coordinates multiple conduit.Conduit instances,
// running them concurrently with a shared junk.Manager.
//
// Use New to create a Host, Add to register conduits, and Run to start
// all registered conduits. Run blocks until the context is cancelled or
// any conduit returns a non-nil error.
package harness