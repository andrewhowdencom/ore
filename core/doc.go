// Package core implements the minimal inference primitive: a single turn
// orchestrator that delegates all LLM-specific work to a provider adapter.
//
// The Loop type is the entry point. Its Turn() method calls the provider,
// appends returned artifacts to state with RoleAssistant, and returns the
// mutated state. It does not handle retries, tool execution, or multi-turn
// looping — those are application-layer concerns.
package core
