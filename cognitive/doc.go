// Package cognitive defines cognitive patterns that drive multi-turn inference
// loops. A cognitive pattern decides when to stop looping based on the state
// of the conversation — for example, ReAct loops while tool results are
// pending, and stops when the assistant produces a final response.
//
// The core abstraction is the Pattern interface:
//
//   - Pattern — Run(ctx, state.State) → (state.State, error)
//
// Concrete implementations include:
//
//   - ReAct — implements the ReAct feedback loop via Run(ctx, state.State).
//
//   - WithVerification — wraps any Pattern and runs quality gates after the
//     inner pattern completes, retrying on failure up to a configurable limit.
//
// Cognitive patterns are conduit-agnostic and stateless. They receive
// state.State as a parameter and return it, without embedding it. The caller
// (typically application-level code) is responsible for IO wiring: reading
// conduit events, appending user messages, routing output events to a
// conduit, and managing status.
package cognitive
