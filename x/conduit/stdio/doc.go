// Package stdio implements a single-shot, unix-filter-style ore conduit.
//
// It reads from an io.Reader, submits a single user turn to a bound
// session.Session, streams assistant artifacts as Markdown blocks to an
// io.Writer, and returns after the turn completes.
//
// This is a deliberate exception to the standard conduit blocking-contract
// (which normally blocks until ctx.Done()) so the conduit can be used in
// CLI pipelines and Unix filters.
//
// The application is responsible for registering and driving the session with
// an engine. Use New(sess, opts...) to create a conduit. Available options
// include WithInput, WithOutput, WithStderr, and WithTracer.
package stdio
