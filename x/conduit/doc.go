// Package conduit defines the Conduit interface, capability constants, and
// descriptors for ore I/O frontends.
//
// I/O Conduits are adapters that translate external events into triggers for the
// application layer and route outputs to external systems. They are not "UIs"
// in the narrow sense. A conduit can be:
//
//   - Interactive — TUI, web interface, Telegram or Discord bot
//   - Event-driven — Webhook receiver, message queue consumer, alert processor
//   - Scheduled — Cron-triggered jobs, periodic report generation
//   - Service-oriented — REST or gRPC endpoint, CLI one-shot, RPC over stdio
//   - Streaming — WebSocket server, SSE endpoint, log tailer
//
// A conduit's contract with the application layer is about ingress events and
// egress actions, not about rendering chat windows.
//
// The Conduit interface is intentionally minimal:
//
//	type Conduit interface {
//	    Start(ctx context.Context) error
//	}
//
// Each concrete implementation (x/conduit/http, x/conduit/tui, and future
// adapters) satisfies this interface at build time. The framework does not
// assume any specific rendering mechanism.
//
// Capability and Descriptor provide a lingua franca for capability discovery
// across ore frontends. Each conduit package exports a Descriptor variable that
// enumerates the well-known capabilities it supports (e.g., event-source,
// render-delta, show-status (structured metadata events via loop.PropertiesEvent),
// accept-text, render-markdown).
//
// Standard Conduit Contract
//
// All conduit packages MUST satisfy the following contract so that framework
// consumers and generators have a single, predictable pattern to follow:
//
//   1. Constructor — New(mgr *session.Manager, opts ...Option) (conduit.Conduit, error)
//
//      The constructor uses the functional-options pattern. It MUST validate that
//      mgr is non-nil. It returns a value that satisfies conduit.Conduit and may
//      be type-asserted to the concrete type for package-specific extensions
//      (e.g., *http.Handler).
//
//   2. Exported Descriptor — var Descriptor = conduit.Descriptor{...}
//
//      Each package exports a package-level Descriptor variable that lists the
//      well-known capabilities the conduit supports. The variable is consumed by
//      documentation generators (cmd/docgen) and static discovery tools.
//
//   3. Sink registration inside Start()
//
//      Conduits that maintain a persistent connection to a session stream MUST
//      subscribe to output events (e.g., stream.Subscribe(...)) inside Start()
//      before entering the blocking loop. Request-driven conduits MAY defer
//      subscription to per-request handlers.
//
//   4. Blocking Start(ctx context.Context) error
//
//      Start MUST block until ctx is cancelled or a fatal error occurs. It MUST
//      return a non-nil error only on fatal startup or runtime errors; clean
//      shutdown on ctx.Done() should return nil.
//
//   5. Graceful shutdown
//
//      On ctx.Done(), the conduit MUST release resources (close channels,
//      shutdown servers, close subscriptions) and return promptly.
//
// This package lives under x/ because the conduit abstraction and capability
// vocabulary are still evolving as new frontend types are explored.
package conduit
