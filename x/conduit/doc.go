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
// Each concrete implementation (x/conduit/tui, and future adapters) satisfies
// this interface at build time. The framework does not assume any specific
// rendering mechanism.
//
// Capability and Descriptor provide a shared vocabulary for describing ore
// frontends. Each conduit package exports a Descriptor variable that enumerates
// the well-known capabilities it supports (for example, event-source,
// render-delta, show-status, accept-text, and render-markdown).
//
// Descriptors are static, package-level metadata intended for documentation and
// discovery tooling. The conduit package does not maintain a registry, provide
// a Supports method, negotiate capabilities at runtime, or verify that the
// advertised capabilities match an implementation. Applications that inspect a
// descriptor must do so explicitly (for example, by checking
// tui.Descriptor.Capabilities).
//
// # Standard Conduit Contract (Session-Based)
//
// All new conduit packages MUST satisfy the following contract so that
// framework consumers and generators have a single, predictable pattern to
// follow. The TUI (`x/conduit/tui`) is the canonical first adopter of this
// contract; see `examples/tui-chat/` for a complete wiring.
//
//  1. Constructor — New(sess *session.Session, opts ...Option) (conduit.Conduit, error)
//
//     The constructor uses the functional-options pattern. It MUST validate
//     that sess is non-nil and return a clear error otherwise. The conduit
//     reads from the session (turns, metadata, Subscribe) but does not own
//     its lifecycle — the application is responsible for registering the
//     session in a session.Registry before constructing the conduit.
//     It returns a value that satisfies conduit.Conduit and may be
//     type-asserted to the concrete type for package-specific extensions.
//
//  2. Egress channel — Events() <-chan session.Event
//
//     Conduits that produce user-initiated events MUST expose a buffered
//     channel via Events(). The application consumes this channel and
//     submits each event to `engine.Engine.Submit`. The channel is closed
//     when Start returns. Per-event provenance is attached via
//     `loop.WithProvenance` on the event's Ctx field so downstream
//     interceptors and tracing layers can attribute the event to the conduit.
//
//  3. Cancellation — WithEventContext(ctx context.Context) Option
//
//     Conduits that react to user interrupts (Ctrl+C, Esc) MUST
//     support registration of a cancellable event context. The conduit
//     invokes the registered func on the user interrupt; the application
//     pairs this with the cancellation primitive appropriate for the
//     engine backing the session. For the session/engine architecture,
//     cancelling the event context propagates into the engine's per-event
//     context and unwinds the running agent's Run. Conduits MUST NOT
//     emit session events to signal cancellation; the cancel path is
//     out-of-band via context propagation.
//
//
//  4. Exported Descriptor — var Descriptor = conduit.Descriptor{...}
//
//     Each package exports a package-level Descriptor variable that lists the
//     well-known capabilities the conduit supports. This is descriptive metadata
//     for documentation and static discovery; it is not part of the Conduit
//     interface and is not enforced at runtime.
//
//  5. Sink registration inside Start()
//
//     Conduits that maintain a persistent connection to a session stream
//     MUST subscribe to output events (e.g., sess.Subscribe(...)) inside
//     Start() before entering the blocking loop. Request-driven conduits
//     MAY defer subscription to per-request handlers.
//
//  6. Blocking Start(ctx context.Context) error
//
//     Start MUST block until ctx is cancelled or a fatal error occurs.
//     It MUST return a non-nil error only on fatal startup or runtime
//     errors; clean shutdown on ctx.Done() should return nil.
//
//  7. Graceful shutdown
//
//     On ctx.Done(), the conduit MUST release resources (close channels,
//     shutdown servers, close subscriptions) and return promptly.
//
// # Legacy Pattern (junk.Manager-Based)
//
// The following conduit packages still follow the legacy *junk.Manager
// pattern that pre-dated this contract:
//
//   - x/conduit/slack
//   - x/conduit/telegram
//
// x/conduit/tui and x/conduit/stdio use the session-based contract.
// x/conduit/http follows the same ownership boundary through a narrow Backend
// interface supplied by the application rather than taking a session directly.
//
// The remaining legacy conduits take `*junk.Manager` and manage session
// lifecycle internally (`mgr.Create`, `mgr.Attach(threadID)`). They submit
// user events through `stream.Submit` and cancel in-flight turns via
// `stream.Cancel`. Migrating each of these to the session-based contract
// is tracked separately. Do NOT use the legacy pattern for new conduits.
//
// This package lives under x/ because the conduit abstraction and capability
// vocabulary are still evolving as new frontend types are explored.
package conduit
