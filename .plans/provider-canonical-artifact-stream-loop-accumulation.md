# Plan: Provider Canonical Artifact Stream with Loop Accumulation

## Objective

Refactor the ore provider contract and OpenAI adapter so that streaming artifacts are emitted in native API arrival order as canonical types (`TextDelta`, `ReasoningDelta`, `ToolCall`, etc.). Push accumulation responsibility from the provider into the `loop.Step.Turn`, which builds ordered, interleaved blocks within the assistant turn. This eliminates the visual "jump" in the TUI where streaming display order diverges from committed turn order.

## Context

The current provider contract (`provider/provider.go`) states: "The adapter is responsible for buffering deltas internally and emitting complete artifacts once the stream finishes." This forces the OpenAI adapter (`provider/openai/openai.go`) to accumulate all `TextDelta` chunks into a single `Text` artifact and all `ReasoningDelta` chunks into a single `Reasoning` artifact, then emit them at stream end in a hardcoded order (text first, reasoning second).

The loop (`loop/loop.go`) separates the mixed delta/complete stream: deltas are forwarded to the `FanOut` for real-time TUI rendering, while complete artifacts are buffered and appended to state after the provider returns. The TUI (`surface/tui/model.go`) accumulates deltas into `streamBlocks` in arrival order, then on `TurnCompleteEvent` renders `msg.turn.Artifacts` in slice order. Because the provider's committed artifacts are reordered, the visual output "jumps" when the turn finalizes.

Key files and their current behavior:
- `provider/provider.go` — Interface contract requires internal buffering
- `provider/openai/openai.go` — `strings.Builder` accumulates text/reasoning; hardcoded emission order at stream end
- `loop/loop.go` — Goroutine separates deltas from complete artifacts via `artifact.Delta` type assertion
- `surface/tui/model.go` — `deltaMsg` handler merges same-kind deltas into `streamBlocks`; `turnMsg` renders `msg.turn.Artifacts` in slice order
- `artifact/artifact.go` — `Text`, `Reasoning`, `TextDelta`, `ReasoningDelta`, `ToolCall`, `ToolCallDelta` types already exist

## Architectural Blueprint

The provider becomes a **thin translator**: it maps each native API chunk to the closest canonical ore artifact type and emits it immediately in arrival order. It accumulates **only** when the native API format doesn't map directly to an ore artifact — specifically, OpenAI's fragmented `tool_calls` deltas must be assembled into a complete `ToolCall` before emission.

The loop's `Step.Turn` becomes the **canonical accumulator**: it receives the artifact stream from the provider, forwards everything to the `FanOut` for subscribers, and simultaneously builds the assistant turn incrementally. Same-kind adjacent deltas merge into one block; a kind switch starts a new block. Complete artifacts (`ToolCall`, etc.) start new blocks. The accumulated turn is appended to state and emitted via `TurnCompleteEvent`.

The TUI receives the same ordered blocks through both paths: deltas via `FanOut` for real-time streaming, and the committed turn via `TurnCompleteEvent`. Because the committed turn's block order matches the streaming block order, there is no visual jump.

## Requirements

1. Update the `Provider` interface contract to state that adapters emit canonical artifact types in native API order, accumulating only when necessary.
2. Remove `strings.Builder` accumulation for text and reasoning from the OpenAI adapter; emit `TextDelta` and `ReasoningDelta` directly per SSE chunk.
3. Keep tool call accumulation in the OpenAI adapter (native API sends fragmented tool call data).
4. Refactor `loop.Step.Turn` to accumulate incoming artifacts into ordered blocks within the current turn.
5. Ensure `TurnCompleteEvent` carries the accumulated turn with blocks in streaming order.
6. Verify the TUI renders committed turns in artifact slice order without hardcoded reordering.
7. All existing tests must continue to pass; new tests must verify the accumulation behavior.
8. Run `go test -race ./...` after every task.

## Task Breakdown

### Task 1: Update provider interface contract documentation
- **Goal**: Update `provider.Provider` documentation and `provider/doc.go` to reflect the new streaming contract.
- **Dependencies**: None.
- **Files Affected**: `provider/provider.go`, `provider/doc.go`
- **New Files**: None.
- **Interfaces**: `Provider.Invoke` signature unchanged; docstring changes from "buffering deltas internally and emitting complete artifacts once the stream finishes" to "emitting canonical artifact types in native API arrival order, accumulating only when the native format does not map directly to an ore artifact type."
- **Validation**: `go test ./provider/...` passes (no code changes, only documentation).
- **Details**: Update the `Invoke` docstring in `provider/provider.go`. Update `provider/doc.go` if it contains contract language. No functional changes.

### Task 2: Refactor OpenAI adapter to emit deltas in native order
- **Goal**: Remove text/reasoning accumulation from the OpenAI provider; emit `TextDelta` and `ReasoningDelta` per SSE chunk in arrival order.
- **Dependencies**: Task 1.
- **Files Affected**: `provider/openai/openai.go`
- **New Files**: None.
- **Interfaces**: `Provider.Invoke` implementation changes internally. `strings.Builder` variables `textContent` and `reasoningContent` are removed. The SSE loop emits `TextDelta` and `ReasoningDelta` directly instead of writing to builders. The post-stream emission of monolithic `Text` and `Reasoning` artifacts is removed. Tool call accumulation (`toolCallAccum`, `toolCalls` map) is preserved.
- **Validation**: `go test ./provider/openai/...` passes. Provider adapter tests using `httptest.Server` verify delta emission order matches mock SSE chunk order.
- **Details**: 
  - Remove `var textContent strings.Builder` and `var reasoningContent strings.Builder`.
  - In the SSE loop, remove `textContent.WriteString(delta.Content)` and `reasoningContent.WriteString(reasoning)`.
  - Keep the `select` blocks that emit `artifact.TextDelta` and `artifact.ReasoningDelta`.
  - Remove the post-stream `if textContent.Len() > 0` and `if reasoningContent.Len() > 0` blocks that emit `artifact.Text` and `artifact.Reasoning`.
  - Keep tool call accumulation and emission unchanged.
  - Update any provider tests that assert on complete artifact emission to assert on delta emission instead.

### Task 3: Refactor loop Step.Turn to accumulate deltas into ordered blocks
- **Goal**: The loop's `Step.Turn` accumulates incoming artifacts into an ordered block list within the current turn, preserving interleaving.
- **Dependencies**: Task 1.
- **Files Affected**: `loop/loop.go`
- **New Files**: None.
- **Interfaces**: `Step.Turn` signature unchanged. Internal accumulation changes: instead of `var completeArtifacts []artifact.Artifact` and the delta/non-delta split, the goroutine maintains a slice of accumulated blocks. `TextDelta` and `ReasoningDelta` are merged into the last block of the same kind, or start a new block if the last block is a different kind or the slice is empty. `ToolCall` (and other complete artifacts) are appended as new blocks. After `Invoke` returns, `st.Append(state.RoleAssistant, accumulatedArtifacts...)` appends the built turn.
- **Validation**: `go test ./loop/...` passes. New or updated tests verify:
  - `TextDelta("Hello") → ReasoningDelta("think") → TextDelta(" world")` produces a turn with `[Text{"Hello"}, Reasoning{"think"}, Text{" world"}]`.
  - Same-kind adjacent deltas merge: `TextDelta("Hello") → TextDelta(" world")` produces `[Text{"Hello world"}]`.
  - `TurnCompleteEvent` carries the accumulated turn with correct artifact order.
  - Deltas are still forwarded to the `FanOut` (existing behavior preserved).
- **Details**:
  - Replace `var completeArtifacts []artifact.Artifact` with a slice that accumulates blocks.
  - In the goroutine, replace the `if _, ok := art.(artifact.Delta); !ok` check with a type switch:
    - `TextDelta`: if last artifact is `Text`, append `Content` to it; else append `Text{Content: d.Content}`.
    - `ReasoningDelta`: if last artifact is `Reasoning`, append `Content` to it; else append `Reasoning{Content: d.Content}`.
    - `ToolCall` (and any other non-delta): append as-is.
  - After `Invoke` returns, `st.Append(state.RoleAssistant, accumulatedArtifacts...)`.
  - The `TurnCompleteEvent` emitted after handlers run carries `last` from `st.Turns()`, which now has the accumulated artifacts.
  - Ensure the goroutine still forwards ALL artifacts to `s.events` for the `FanOut` (this must not change).

### Task 4: Align TUI turn rendering with artifact slice order
- **Goal**: Verify the TUI's `turnMsg` handler renders `msg.turn.Artifacts` in slice order without hardcoded reasoning-before-text logic.
- **Dependencies**: Task 2, Task 3.
- **Files Affected**: `surface/tui/model.go`
- **New Files**: None.
- **Interfaces**: `model.Update(turnMsg)` already iterates `msg.turn.Artifacts` in order. The `switch a := art.(type)` cases for `artifact.Text` and `artifact.Reasoning` are already in slice order. No hardcoded reordering is present. This task is primarily verification and minor cleanup if any implicit ordering assumptions are found.
- **Validation**: `go test ./surface/tui/...` passes. `TestModel_Update_Delta_Interleaved` already verifies interleaved delta handling. Add or update a test that verifies `turnMsg` with interleaved `[Text, Reasoning, Text]` renders blocks in that order.
- **Details**:
  - Review `model.Update(turnMsg)` to confirm the artifact switch is purely slice-order driven.
  - If any implicit ordering (e.g., reasoning always rendered before text) exists in `view.go` or rendering logic, remove it.
  - Add `TestModel_Update_Turn_Interleaved` to verify committed turn block order matches delta order.

### Task 5: Update and add tests
- **Goal**: Ensure all tests reflect the new provider/loop behavior and verify the ordering fix.
- **Dependencies**: Task 2, Task 3, Task 4.
- **Files Affected**: `loop/loop_test.go`, `provider/openai/openai_test.go` (if exists), `surface/tui/model_test.go`
- **New Files**: None.
- **Interfaces**: Test assertions change from expecting monolithic `Text`/`Reasoning` complete artifacts to expecting interleaved `Text`/`Reasoning` blocks.
- **Validation**: `go test -race ./...` passes.
- **Details**:
  - `loop/loop_test.go`: Update `TestStep_Turn_AppendsArtifacts`, `TestStep_Turn_AppendsReasoningArtifact`, `TestStep_Turn_OutputEvents`, and other tests that assert on `last.Artifacts` order. The tests currently expect `[Text, ToolCall]` or `[Text, Reasoning]`; with the mock provider emitting in a specific order, the accumulated turn should match that order.
  - `loop/loop_test.go`: Add `TestStep_Turn_AccumulatesInterleavedDeltas` verifying `TextDelta → ReasoningDelta → TextDelta` produces `[Text, Reasoning, Text]`.
  - `surface/tui/model_test.go`: Add `TestModel_Update_Turn_Interleaved` (or update existing `TestModel_Update_Turn` / `TestModel_Update_Turn_PreservesReasoning`).
  - Provider tests: If `provider/openai/openai_test.go` exists, update SSE mock tests to assert delta emission order. If not, this task may be minimal for the provider package.

### Task 6: End-to-end validation
- **Goal**: Run the full test suite and verify the TUI example compiles and behaves correctly.
- **Dependencies**: Task 5.
- **Files Affected**: `examples/tui-chat/main.go` (verification only, no changes expected)
- **New Files**: None.
- **Interfaces**: None.
- **Validation**: `go test -race ./...` passes. `go build ./examples/tui-chat` succeeds.
- **Details**:
  - Run `go test -race ./...`.
  - Run `go build ./...` to ensure all packages compile.
  - Verify `examples/tui-chat` compiles.
  - Review the `examples/tui-chat/main.go` event loop to confirm it doesn't need changes (the `react.Run` → `Step.Turn` path should work transparently with the new accumulation).

## Dependency Graph

- Task 1 → Task 2 (Task 2 depends on Task 1)
- Task 1 → Task 3 (Task 3 depends on Task 1)
- Task 2 || Task 3 (parallel once Task 1 is complete)
- Task 2 + Task 3 → Task 4 (Task 4 depends on both)
- Task 4 → Task 5 (Task 5 depends on Task 4)
- Task 5 → Task 6 (Task 6 depends on Task 5)

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Provider tests don't exist for OpenAI adapter | Medium | High | If `provider/openai/openai_test.go` is missing, Task 2 validation relies on loop-level integration tests. Consider adding provider tests as part of Task 2 or accepting loop-test coverage. |
| `concatText` newline joining between multiple `Text` artifacts in one turn | Low | Low | Acceptable edge case per ideation: LLMs rarely interleave text and reasoning, and adjacent text blocks joined by `\n` is semantically acceptable. |
| `FanOut` backpressure / dropped deltas | Medium | Medium | The `FanOut` delivers non-blocking; slow subscribers may drop events. This is existing behavior and unchanged by this plan. Documented in `loop/fanout.go`. |
| Tool call accumulation in provider still needed | Medium | Low | Tool call assembly is the one case where provider accumulation is unavoidable. This is explicitly preserved in Task 2. |
| TUI `streamBlocks` and committed turn still diverge if logic differs | Medium | Medium | Task 4 verifies the TUI renders in slice order. The TUI's `streamBlocks` accumulation mirrors the loop's accumulation (same-kind merge, kind-switch = new block), so they should stay consistent. |

## Validation Criteria

- [ ] `go test -race ./...` passes after every task.
- [ ] `go build ./...` passes after every task.
- [ ] Provider interface contract documentation no longer references "buffering deltas internally and emitting complete artifacts once the stream finishes."
- [ ] OpenAI provider does not use `strings.Builder` for text or reasoning accumulation.
- [ ] OpenAI provider emits `TextDelta` and `ReasoningDelta` in SSE chunk arrival order.
- [ ] Loop `Step.Turn` accumulates `TextDelta`/`ReasoningDelta` into ordered blocks with same-kind merging.
- [ ] `TurnCompleteEvent` carries a turn whose artifact order matches the streaming delta order.
- [ ] TUI `turnMsg` renders blocks in `msg.turn.Artifacts` slice order.
- [ ] New or updated tests verify interleaved `TextDelta → ReasoningDelta → TextDelta` produces `[Text, Reasoning, Text]` in both loop and TUI.
