# Plan: Forward Complete Artifacts to Event Stream

## Objective

Fix `loop.Turn()` so that all artifacts (both delta and complete) emitted by the provider are forwarded to the embedded `FanOut` event stream. The `artifact.Delta` marker interface should only control whether an artifact is persisted to state, not whether it is visible to subscribers. This aligns the implementation with the plan's stated intent that "artifact.Artifact values are emitted directly as OutputEvent" and with the `conduit/tui` design, which type-switches on `artifact.Artifact` generically.

## Context

From ideation and discovery of the ore codebase:

- **`loop/loop.go`** (`Turn()` method): The goroutine that reads from the provider channel currently type-asserts `artifact.Delta` to decide whether to forward to `s.events`. Only delta artifacts are emitted; complete artifacts are silently buffered in `completeArtifacts` and never reach subscribers.
- **`conduit/tui/tui.go`**: The TUI conduit's event-reading goroutine type-switches on `artifact.Artifact` (the base interface), structurally prepared to receive any artifact type. The fact that it never receives complete artifacts from `loop.Turn()` is a latent mismatch.
- **`loop/loop_test.go`**: Tests such as `TestStep_Turn_OutputEvents` subscribe to delta kinds and verify delta delivery. They do not assert that complete artifacts are also emitted — because they currently are not. `TestStep_Turn_OutputEvents_OnlyCompletes` subscribes to `"text_delta"` and `"turn_complete"`, so a complete `Text` artifact is dropped by the `FanOut` (no matching subscriber) — masking the fact that `loop.Turn()` never sent it.
- **`.plans/unify-provider-streaming.md`**: States "artifact.Artifact values are emitted directly as OutputEvent (they already satisfy `Kind() string`)." and "The loop uses this [Delta interface] to route ephemeral content to conduits immediately while buffering complete artifacts for state." The implementation interpreted the second sentence as filtering the event stream, but the design intent was dual-path: all artifacts to conduits, only non-deltas to state.
- **`provider/openai/openai.go`**: The OpenAI adapter already emits complete artifacts (e.g., `Text`, `Reasoning`, `ToolCall`) to the provider channel at the end of streaming. They simply never reach subscribers because `loop.Turn()` absorbs them.

## Architectural Blueprint

The fix is a single, focused change to the artifact-routing logic in `loop.Turn()`:

1. **All artifacts flow to the event stream**: The goroutine reading from the provider channel forwards every artifact to `s.events` immediately as it arrives.
2. **Delta marker controls state only**: The `artifact.Delta` type assertion is still used, but only to decide whether the artifact is also buffered in `completeArtifacts` for the batched `st.Append()` at the end. Deltas are forwarded but not buffered; complete artifacts are forwarded AND buffered.
3. **No change to signatures or interfaces**: `Step`, `Turn()`, `Subscribe()`, `FanOut`, `Provider`, and `Handler` interfaces remain unchanged. Conduits that already subscribe to specific `Kind()`s continue to work. Conduits that want to receive complete artifacts can subscribe to their kinds (e.g., `"text"`, `"tool_call"`, `"usage"`).
4. **Ordering preserved**: Because the provider emits deltas first (during streaming) and completes last (after assembly), subscribers that subscribe to both delta and complete kinds will see deltas before the corresponding complete artifact — which is the correct temporal ordering.

## Requirements

1. `loop.Turn()` goroutine must forward every artifact to `s.events` as it arrives on the provider channel, without filtering by `artifact.Delta`.
2. The `artifact.Delta` check must still prevent delta artifacts from being buffered in `completeArtifacts` for state.
3. Tests must verify that complete artifacts are emitted to subscribers who subscribe to the matching `Kind()`.
4. Tests must verify that delta artifacts continue to be emitted and are NOT appended to state.
5. `go test -race ./...` must pass after all changes.

## Task Breakdown

### Task 1: Forward All Artifacts to Event Stream
- **Goal**: Change `loop.Turn()` so that every artifact read from the provider channel is sent to `s.events`, not just delta artifacts.
- **Dependencies**: None.
- **Files Affected**: `loop/loop.go`
- **New Files**: None.
- **Interfaces**: No new or modified signatures.
- **Validation**: `go test ./loop/...` passes after updating the tests in Task 2. (Run Task 2's test updates alongside this change, as they are tightly coupled.)
- **Details**: In the goroutine inside `Turn()`, change the `if _, ok := art.(artifact.Delta); ok { ... } else { ... }` logic to: (a) send every artifact to `s.events` unconditionally (keeping the existing `select` with `ctx.Done()`); (b) only append to `completeArtifacts` if the artifact does NOT satisfy `artifact.Delta`. The delta-vs-complete split for state buffering must remain. Non-blocking semantics for `s.events` sends must be preserved — use the same `select` pattern with `ctx.Done()` and do NOT add a `default` case (the channel is buffered; blocking sends are acceptable here because the FanOut is non-blocking).

### Task 2: Update Tests to Verify Complete Artifact Event Emission
- **Goal**: Update existing loop tests and add new tests to assert that complete artifacts are forwarded to subscribers through the event stream.
- **Dependencies**: Task 1.
- **Files Affected**: `loop/loop_test.go`
- **New Files**: None.
- **Interfaces**: None.
- **Validation**: `go test ./loop/...` passes, `go test -race ./...` passes.
- **Details**:
  1. Update `TestStep_Turn_OutputEvents`: Add `"text"` to the subscription kinds (`Subscribe("text_delta", "text", "turn_complete")`). Expect 4 events instead of 3: `TextDelta{"wor"}`, `TextDelta{"ld"}`, `Text{"world!"}`, `TurnCompleteEvent`. Verify the `Text` artifact's content and that it arrives before `TurnCompleteEvent`.
  2. Add `TestStep_Turn_CompleteArtifactEvent` (or similar): Create a `Step`, subscribe to `"text"` and `"turn_complete"`, run `Turn()` with a provider that emits only `artifact.Text{Content: "hello"}`. Collect events and assert that both the `Text` event and `TurnCompleteEvent` are received (2 events). The `Text` artifact should also be in the assistant turn's artifacts.
  3. Add `TestStep_Turn_ErrorEmitsCompleteArtifacts` (or similar): Create a provider that emits `artifact.Text{Content: "partial"}` followed by returning an error. Subscribe to `"text"` and `"error"`. Assert that the `Text` event is received (the complete artifact WAS forwarded before the error), but state was NOT mutated (no assistant turn appended).
  4. Verify that existing tests that do NOT subscribe to complete artifact kinds (e.g., `TestStep_Turn_OutputEvents_OnlyCompletes`, `TestStep_Turn_DeltasDroppedWithoutSubscriber`, `TestStep_Turn_ErrorEvent`) still pass without modification — the FanOut should drop events whose kinds do not match the subscription, as before.

### Task 3: Update Loop Package Documentation
- **Goal**: Update `loop/doc.go` to clarify that all artifacts (delta and complete) flow through the event stream, and that `artifact.Delta` controls state persistence only.
- **Dependencies**: Task 1.
- **Files Affected**: `loop/doc.go`
- **New Files**: None.
- **Interfaces**: None.
- **Validation**: Documentation is consistent with code behavior; no build or test impact.
- **Details**: Update the package comment to state that the `Step` forwards all artifacts from the provider to `Subscribe()` channels immediately as they arrive, and that the `Delta` marker interface is used solely to distinguish ephemeral artifacts that must not be persisted to state. Remove or correct any language that implies only deltas are forwarded to subscribers.

## Dependency Graph

- Task 1 → Task 2 (tests depend on the code change)
- Task 1 → Task 3 (docs depend on the code change)
- Task 2 || Task 3 (tests and docs are parallelizable once the implementation is in place)

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Conduits subscribing to both delta and complete kinds receive redundant content | Low | Medium | By design — conduits control their subscription. The TUI already subscribes only to delta kinds and accumulates content itself. No conduits in the current codebase subscribe to complete artifact kinds. |
| Complete artifacts dropped by FanOut if no subscriber asked for that kind | Low | High | Same behavior as deltas today. The FanOut already documents non-blocking delivery with fixed buffer and kind-based filtering. |
| Test changes accidentally alter assertions for unrelated behavior | Medium | Low | Keep existing tests unchanged where possible. Only add new assertions or new test cases. Run `go test -race ./...` before and after. |
| Provider adapters rely on loop's buffering order for side effects | Low | Low | Provider adapters have no visibility into loop buffering. They only write to a channel. The change is purely in loop routing. |

## Validation Criteria

- [ ] `go test ./loop/...` passes.
- [ ] `go test -race ./...` passes.
- [ ] `go build ./...` passes.
- [ ] A subscriber to `"text"`, `"tool_call"`, or `"usage"` receives those complete artifact kinds as `OutputEvent` values during `Turn()`.
- [ ] A subscriber to `"text_delta"`, `"reasoning_delta"`, or `"tool_call_delta"` continues to receive delta artifacts during `Turn()`.
- [ ] Delta artifacts are NOT appended to state (only complete artifacts are).
- [ ] On provider error after emitting a complete artifact, the complete artifact IS forwarded to `s.events` (if a subscriber exists), but state is NOT mutated.
- [ ] `loop/doc.go` accurately documents that all artifacts flow through the event stream and `Delta` controls state persistence only.
