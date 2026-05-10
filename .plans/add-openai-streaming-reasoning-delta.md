# Plan: Add ReasoningDelta Emission to OpenAI Streaming Provider

## Objective

Add support for emitting `artifact.ReasoningDelta` during streaming in the OpenAI provider (`provider/openai/openai.go`). The TUI surface already supports rendering reasoning deltas (PR #28), but no provider currently emits them during streaming. This plan implements the missing OpenAI streaming integration so that reasoning content from models like o1 and o3-mini is displayed in real time.

## Context

- **File**: `provider/openai/openai.go` — The `InvokeStreaming` method processes streaming chunks from the OpenAI SDK but only handles `delta.Content` (text) and `delta.ToolCalls`. It ignores reasoning-related fields.
- **File**: `provider/openai/openai.go` — The non-streaming `Invoke` method already extracts reasoning content from `msg.JSON.ExtraFields["reasoning_content"]` and appends `artifact.Reasoning` to the returned artifacts (lines 228–231).
- **File**: `surface/tui/model.go` — The TUI `Update` method handles `artifact.ReasoningDelta` by writing it to `reasoningStreamBuffer` (already implemented in PR #28).
- **File**: `surface/tui/view.go` — The TUI `View` method renders `reasoningStreamBuffer` with a `Thinking:` label in faint/italic style (already implemented).
- **File**: `artifact/artifact.go` — `ReasoningDelta` type already exists with `Kind() string { return "reasoning_delta" }`.
- **SDK behavior**: The `github.com/openai/openai-go` SDK's `ChatCompletionChunkChoiceDelta` struct does not have a direct `ReasoningContent` field, but it has `JSON.ExtraFields` which captures unknown JSON fields during unmarshalling. The non-streaming method already successfully uses `msg.JSON.ExtraFields["reasoning_content"]`.
- **Test pattern**: Existing streaming tests in `provider/openai/openai_test.go` use `httptest.Server`-style mock responses with SSE-formatted JSON bodies. The SDK parses these into `ChatCompletionChunk` objects.

## Architectural Blueprint

The solution is a localized change to the OpenAI provider's streaming path. No new components or packages are needed.

1. **In `InvokeStreaming`**, add reasoning detection alongside the existing text and tool call processing:
   - After processing `delta.Content`, inspect `delta.JSON.ExtraFields["reasoning_content"]` (the same field name used by the non-streaming path).
   - If present and non-empty, emit `artifact.ReasoningDelta{Content: reasoning}` to `deltasCh`.
   - Accumulate reasoning content into a `strings.Builder` (mirroring `textContent`).

2. **In final artifact assembly**, append `artifact.Reasoning` if reasoning content was accumulated, mirroring the non-streaming `Invoke` behavior.

3. **In tests**, add an SSE stream containing chunks with `reasoning_content` in the delta, and assert that:
   - `ReasoningDelta` artifacts are emitted on the channel.
   - The final artifacts slice includes `artifact.Reasoning`.

## Requirements

- The OpenAI provider's `InvokeStreaming` inspects streaming chunks for reasoning content via `delta.JSON.ExtraFields`.
- When reasoning content is present in a chunk, it emits `artifact.ReasoningDelta` on `deltasCh`.
- Reasoning content is accumulated across chunks and returned as `artifact.Reasoning` in the final artifacts slice.
- A test verifies that `ReasoningDelta` is emitted when the mock SSE stream contains reasoning chunks.
- The existing test suite (`go test -race ./...`) continues to pass.

## Task Breakdown

### Task 1: Emit ReasoningDelta in OpenAI InvokeStreaming
- **Goal**: Modify `provider/openai/openai.go` to detect reasoning content in streaming chunks and emit `artifact.ReasoningDelta`.
- **Dependencies**: None.
- **Files Affected**: `provider/openai/openai.go`
- **New Files**: None.
- **Interfaces**: No new interfaces. Existing `artifact.ReasoningDelta` type is used.
- **Validation**: `go test -race ./provider/openai/...` passes.
- **Details**:
  1. Add a `reasoningContent strings.Builder` variable alongside `textContent` in `InvokeStreaming`.
  2. In the `for stream.Next()` loop, after the `delta.Content` block, add a block that:
     - Checks `delta.JSON.ExtraFields["reasoning_content"]`.
     - Unmarshals the raw field value to a `string`.
     - If non-empty, writes it to `reasoningContent` and emits `artifact.ReasoningDelta{Content: reasoning}` to `deltasCh` (with context cancellation check).
  3. In the final artifact assembly section, change the `if len(toolCalls) > 0 { ... } else if textContent.Len() > 0 { ... }` chain to:
     - If `len(toolCalls) > 0`, append tool calls (unchanged).
     - Else if `textContent.Len() > 0`, append `artifact.Text` (unchanged).
     - Separately, if `reasoningContent.Len() > 0`, append `artifact.Reasoning{Content: reasoningContent.String()}`.
     This mirrors the non-streaming `Invoke` method which appends reasoning independently.

### Task 2: Add Streaming Reasoning Tests
- **Goal**: Add tests verifying `ReasoningDelta` emission and final `artifact.Reasoning` inclusion.
- **Dependencies**: Task 1.
- **Files Affected**: `provider/openai/openai_test.go`
- **New Files**: None.
- **Interfaces**: No new interfaces.
- **Validation**: `go test -race ./provider/openai/...` passes. The new test asserts correct behavior.
- **Details**:
  1. Add `TestProviderInvokeStreaming_WithReasoning`:
     - Construct an SSE body with at least two chunks. Each chunk's `delta` contains both `"content":"..."` and `"reasoning_content":"..."`.
     - Use the existing mock transport pattern (`mockTransport` + `mockClient`).
     - Invoke `InvokeStreaming` with a buffered channel.
     - Assert that the channel contains the expected interleaved `TextDelta` and `ReasoningDelta` artifacts.
     - Assert that the returned `artifacts` slice includes `artifact.Reasoning` with the accumulated content.
  2. Add `TestProviderInvokeStreaming_ReasoningOnly`:
     - Construct an SSE body where chunks contain only `reasoning_content` and no `content`.
     - Assert that `ReasoningDelta` artifacts are emitted.
     - Assert that the final `artifacts` contains `artifact.Reasoning` and no `artifact.Text`.
  3. Follow the existing table-driven style where appropriate, and reuse the SSE string-building pattern from `TestProviderInvokeStreaming_Success`.

## Dependency Graph

- Task 1 → Task 2 (Task 2 depends on Task 1)

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| The OpenAI SDK does not capture `reasoning_content` in `ExtraFields` for streaming deltas | High | Low | The SDK's `UnmarshalJSON` for `ChatCompletionChunkChoiceDelta` uses the same JSON infrastructure as the non-streaming message, which already works. If it fails, investigate using `delta.RawJSON()` directly. |
| Models that stream reasoning and text interleave them unpredictably | Medium | Medium | The implementation accumulates both independently, which matches the non-streaming behavior. |
| Test SSE bodies don't match actual API response structure for reasoning models | Low | Low | Use the same SSE format as existing tests, adding `reasoning_content` alongside `content` in the `delta` object. |

## Validation Criteria

- [ ] `go test -race ./provider/openai/...` passes with new tests.
- [ ] `go test -race ./...` passes for the entire repository.
- [ ] `TestProviderInvokeStreaming_WithReasoning` verifies `ReasoningDelta` is emitted on the channel.
- [ ] `TestProviderInvokeStreaming_WithReasoning` verifies final artifacts include `artifact.Reasoning`.
- [ ] `TestProviderInvokeStreaming_ReasoningOnly` verifies reasoning-only streams produce correct artifacts.
- [ ] The TUI example (`examples/tui-chat` or equivalent) displays reasoning content with the `Thinking:` label when using a reasoning model.
