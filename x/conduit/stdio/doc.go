// Package stdio implements a single-shot, unix-filter-style ore conduit.
//
// It reads from an io.Reader, submits a single user message through a
// session.Manager, streams assistant artifacts as Markdown blocks to an
// io.Writer, and returns after the turn completes.
//
// This is a deliberate exception to the standard conduit blocking-contract
// (which normally blocks until ctx.Done()) so the conduit can be used in
// CLI pipelines and Unix filters.
//
// Use New(mgr, opts...) to create a conduit. Available options include
// WithInput, WithOutput, and WithThreadID.
package stdio
