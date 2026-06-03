// Package loop implements the single-turn execution primitive for ore.
// It provides a Step type that invokes a provider, distributes all artifacts
// (both delta and complete) to subscribers via an embedded FanOut, and runs
// registered artifact handlers on the complete response.
//
// Why use transforms?
//
// Transforms inject virtual content (system prompts, guardrails, dynamic
// context) into the provider's view at inference time without mutating the
// persistent conversation history. This keeps the buffer append-only while
// still shaping every model call.
//
// Options include:
//
//   - WithTransforms — modify the state view presented to the provider
//     during inference. Transforms run before each provider call in Turn(),
//     composing in registration order. They must not mutate the underlying
//     persistent buffer; use state.Prepend or state.NewView to create derived
//     views that prepend virtual turns. See x/systemprompt for a reusable
//     transform that injects a system prompt without touching history.
//   - WithHandlers — run artifact handlers on the complete response.
//
// Step is the single canonical single-turn execution primitive with
// optional, opt-in capabilities via functional options. A Step with no
// options is valid for non-streaming, non-handler use cases.
//
// Conduits subscribe to specific artifact kinds via Step.Subscribe(),
// receiving ArtifactEvent wrappers (which satisfy OutputEvent via Kind())
// as they are emitted by the provider. Each ArtifactEvent carries the
// underlying artifact and a context.Context for routing metadata. The
// artifact.Delta marker interface
// controls whether an artifact is persisted to state; it does NOT filter
// event-stream visibility. All artifacts are forwarded to subscribers.
//
// Output event taxonomy:
//   - ArtifactEvent wraps a provider artifact (text, reasoning, tool_call,
//     etc.) and is emitted for every chunk received from the provider.
//   - TurnCompleteEvent fires after each individual turn is fully
//     accumulated, carrying the complete turn for state persistence
//     and non-streaming delivery (e.g. Slack, Telegram).
//   - LifecycleEvent signals phase transitions: "submitted" (message
//     accepted, waiting for provider) and "streaming" (first artifact
//     arrived). Callers (e.g. session.Stream.Process) may emit "done" to
//     signal pipeline completion. Conduits observe this to drive UI
//     state without inferring lifecycle from data events.
//   - ErrorEvent fires when an individual turn fails inside the provider
//     or a registered handler.
//   - PropertiesEvent carries ambient, persistent metadata as a map of
//     key-value pairs (e.g. thread_id, token counts, model name). It is
//     emitted by any component holding a *session.Stream and delivered to
//     all subscribers through the per-session FanOut.
package loop
