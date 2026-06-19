// Package request provides a thread-scoped, one-shot capture facility for
// the HTTP request and response of the next provider invocation.
//
// It is the first subcommand of the broader x/debug family. Future debug
// targets (state, tools, etc.) will live in sibling packages and register
// their own slash subcommands under the same /debug namespace.
//
// # Design
//
// Dumper wraps an http.RoundTripper and is always installed, but noop when
// disarmed: the hot path cost is a single sync.Map.Load. Calling
// Dumper.Enable(threadID) arms the dumper for that thread; the next
// RoundTrip whose request context carries that thread ID (set by the loop
// via loop.WithThreadID) performs the capture and atomically disarms.
//
// The captured request and response are written to a single file:
//
//	<appName>.request.<RFC3339>.log
//
// with sections delimited by === REQUEST === and === RESPONSE ===
// markers. The file is opened on Enable() and closed when the wrapped
// response body's Close() fires. If a request reaches the wire without a
// thread ID on its context (e.g. a direct provider call outside the loop)
// the capture is silently skipped — the dumper never grabs the wrong
// thread's traffic.
//
// The format is "headers + raw body" written manually for both sides so
// streaming responses work via io.TeeReader; the wire still sees a
// streaming body and the dumper captures a copy. There is no redaction —
// the captured file contains the full Authorization header and request
// payload. This is a debug tool: trust the user.
//
// # Slash command
//
// Bind(reg, dumper) registers a /debug request handler with the
// application's slash.Registry. The handler recovers the active thread ID
// from the slash.Command's stream and arms the dumper for it.
package request