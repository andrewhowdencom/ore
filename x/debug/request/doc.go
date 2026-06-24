// Package request is the first subcommand of the x/debug family. It
// provides a thread-scoped, one-shot capture facility for the HTTP
// request and response of the next provider invocation, so a user
// running an interactive session can inspect the exact wire payload
// when the rendered UI disagrees with what the model actually saw.
//
// # Usage
//
// Construct a Dumper at application startup, wrap a single
// *http.Client with it, and pass the client to both Anthropic and
// OpenAI providers:
//
//	dbg, _ := request.New("myapp")
//	defer dbg.Close()
//
//	client := &http.Client{Transport: dbg.Wrap(http.DefaultTransport)}
//	anthropic.New(anthropic.WithHTTPClient(client))
//	openai.New(openai.WithHTTPClient(client))
//
// In a session, the user types:
//
//	/debug request
//
// which arms the dumper for the active thread. The next provider
// request from that thread is captured to a file and the dumper
// auto-disarms:
//
//	<outputDir>/myapp.request.<RFC3339>.log
//
// # Design
//
// Dumper wraps an http.RoundTripper and is always installed, but
// noop when disarmed: the hot path cost is a single sync.Map.Load.
// Calling Dumper.Enable(threadID) arms the dumper for that thread;
// the next RoundTrip whose request context carries that thread ID
// (set by the loop via loop.WithThreadID) performs the capture and
// atomically disarms.
//
// The capture is one-shot: only the next round trip from the armed
// thread is captured. After that round trip's response body is
// closed, the file is flushed and closed. A second /debug request
// arms the dumper again for the next round trip.
//
// # File format
//
// Each capture produces one file with two sections:
//
//	=== REQUEST ===
//	<METHOD> <path>?<query>
//	Header-Name: Header-Value
//	...
//
//	<raw request body>
//
//	=== RESPONSE ===
//	<status line, e.g. "200 OK">
//	Header-Name: Header-Value
//	...
//
//	<raw response body>
//
// # Streaming
//
// Streaming responses are captured via io.TeeReader on the
// response body. The wire still sees a streaming body; the dumper
// captures a copy. Streaming request bodies are not directly
// supported — the request body is drained to memory and restored
// for the real transport, which is the same behavior the framework
// uses elsewhere when it needs to inspect the outgoing payload.
//
// # Thread scoping
//
// Capture is keyed on the thread ID pulled from the request
// context via loop.ThreadIDFrom. A request that reaches the wire
// without a thread ID on its context (e.g. a direct provider call
// outside the loop) is silently passed through without touching
// any armed slot. This is the safe default: the dumper never
// grabs traffic for the wrong thread.
//
// # No redaction
//
// The capture file contains the full Authorization header and
// request payload. The dumper does not strip auth headers or any
// other sensitive fields — it is a debug tool, and trust in the
// user is the contract. The file is written to the configured
// output directory (default: current working directory) which the
// application is responsible for protecting.
//
// # Slash command
//
// Bind(reg, dumper) registers the /debug command with the
// application's slash.Registry. The handler recovers the active
// thread ID from the slash.Command's stream and arms the dumper
// for it. The handler is a small dispatcher: only the "request"
// subcommand is implemented; future debug subcommands (state,
// tools, ...) will live in sibling x/debug/<name> packages and
// may register their own Handlers under the same "debug" name.
//
// # Cleanup
//
// Dumper.Close() releases any capture file that the wire's
// response body Close() did not release (the rare case where the
// wire leaks a body). In normal operation, the file lifecycle is
// driven by the response body's Close.
package request
