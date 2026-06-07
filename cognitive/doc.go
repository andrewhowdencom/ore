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
//     It depends on loop.TurnRunner (not the concrete *loop.Step) so it can
//     be composed with any component that can run a single inference turn.
//
//   - WithVerification — wraps any Pattern and runs quality gates after the
//     inner pattern completes, retrying on failure up to a configurable limit.
//     It depends on loop.TurnSubmitter to inject system turns containing
//     verification reports back into the conversation.
//
// Middleware composition is modelled as nesting: patterns depend on the
// narrowest loop interface they need (TurnRunner or TurnSubmitter), and
// callers pass a loop.TurnExecutor (which satisfies both) when wiring
// them together. For example:
//
//     WithVerification(ReAct(inner), submitter)
//
// where ReAct requires a TurnRunner and WithVerification requires a
// TurnSubmitter. The concrete *loop.Step satisfies both interfaces.
//
// Cognitive patterns are conduit-agnostic and stateless. They receive
// state.State as a parameter and return it, without embedding it. The caller
// (typically application-level code) is responsible for IO wiring: reading
// conduit events, appending user messages, routing output events to a
// conduit, and managing status.
//
// See also: loop package — the single-turn execution primitive, EventBus /
// Pipeline decomposition, and the TurnRunner / TurnSubmitter / TurnExecutor
// interfaces.
package cognitive
