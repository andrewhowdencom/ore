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
//     persistent buffer; use state.NewVirtualTurnState to create derived
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
// underlying artifact and an EventContext for routing metadata. The
// artifact.Delta marker interface
// controls whether an artifact is persisted to state; it does NOT filter
// event-stream visibility. All artifacts are forwarded to subscribers.
//
// Output event lifecycle:
//   - TurnCompleteEvent fires after each individual assistant turn,
//     carrying the turn content for incremental rendering.
//   - ProcessCompleteEvent fires once per user-initiated interaction
//     when the entire pipeline (including all tool-call loops) finishes.
//     It carries the final error state and is the correct signal for
//     lifecycle actions (audio notifications, typing indicator dismissal).
//   - ErrorEvent fires when an individual turn fails inside the provider
//     or a registered handler.
//   - StatusEvent carries ambient, persistent status information as a
//     map of key-value pairs (e.g. thread_id, token counts, model name).
//     Any component with access to a *session.Stream can emit it, and it
//     is delivered to all subscribers through the per-session FanOut.
package loop
