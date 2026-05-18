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
// render-delta, show-status, accept-text, render-markdown).
//
// This package lives under x/ because the conduit abstraction and capability
// vocabulary are still evolving as new frontend types are explored.
package conduit
