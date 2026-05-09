// Package cognitive defines cognitive patterns that drive multi-turn inference
// loops. A cognitive pattern decides when to stop looping based on the state
// of the conversation — for example, ReAct loops while tool results are
// pending, and stops when the assistant produces a final response.
//
// Cognitive patterns are surface-agnostic and stateless. They receive
// state.State as a parameter and return it, without embedding it. The caller
// (typically application-level code) is responsible for IO wiring: reading
// surface events, appending user messages, routing output events to a
// surface, and managing status.
package cognitive
