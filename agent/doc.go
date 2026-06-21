// Package agent provides the agent bundle primitive. An Agent is a
// configured, self-sufficient unit of inference: it owns the provider,
// model spec, transforms, handlers, cognitive pattern, tracer, and
// (optionally) a state binding. Callers configure the agent at
// construction and then use Run to perform inference.
//
// The agent is reusable: many Run calls share the same internal step.
// The agent is composable: any agent can be wrapped as a tool.Tool by
// a parent agent (see x/subagent), or exercised by a benchmark harness
// (see cmd/benchmark).
//
// Differences between agent kinds (ReAct, SingleShot, Verified) live in
// the configured pattern, not in the agent type. All agents are
// agent.Agent.
//
// When WithState is bound, Run auto-appends the produced turn to the
// bound state. Callers that want a fresh state per Run should not bind
// state or should call LoadTurns before each Run.
//
// Lifecycle: an agent constructed by New owns an internal *loop.Step.
// Close stops the step and closes any subscriber channels. Close is
// safe to call multiple times.
package agent
