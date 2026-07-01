// Package cognitive defines cognitive patterns that drive multi-turn inference
// loops. A cognitive pattern decides when to stop looping based on the state
// of the conversation — for example, ReAct loops while tool results are
// pending, and stops when the assistant produces a final response.
//
// The core abstraction is the Pattern interface:
//
//   - Pattern — Run(ctx, ledger.State) → (ledger.State, error)
//
// Concrete implementations include:
//
//   - ReAct — implements the ReAct feedback loop via Run(ctx, ledger.State).
//     It depends on loop.TurnRunner (not the concrete *loop.Step) so it can
//     be composed with any component that can run a single inference turn.
//
// Cognitive patterns are conduit-agnostic and stateless. They receive
// ledger.State as a parameter and return it, without embedding it. The caller
// (typically application-level code) is responsible for IO wiring: reading
// conduit events, appending user messages, routing output events to a
// conduit, and managing status.
//
// Verification wrappers — Pattern implementations that run quality gates
// after the inner pattern completes and inject a retry turn on failure —
// live in the x/verifier package, alongside the concrete Verifier primitives
// they compose. See verifier.WithVerification.
//
// See also: loop package — the single-turn execution primitive, EventBus /
// Pipeline decomposition, and the TurnRunner / TurnSubmitter / TurnExecutor
// interfaces.
package cognitive
