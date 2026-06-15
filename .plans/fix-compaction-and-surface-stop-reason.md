# Plan: Fix Compaction Truncation and Surface Stop Reason

## Objective

Fix the immediate compaction bug — `SummarizeStrategy` produces a 1-token "##" summary on the Anthropic adapter because the provider defaults `max_tokens` to 1 and the strategy does not pass any invoke options — and address the structural cause: neither LLM provider adapter surfaces `stop_reason` / `finish_reason` to the framework, so any caller that hits a hard cap on response length silently receives a partial result. Introduce a canonical `artifact.StopReason` that both adapters emit on the streaming channel, drop the broken `MaxTokens: 1` default in the Anthropic adapter, and have `SummarizeStrategy` pass an explicit long-form budget and surface truncation as an error.

## Context

Three layers compound into the user-visible "compaction returns `##`" symptom:

1. **Provider default.** `x/provider/anthropic/anthropic.go:363` sets `MaxTokens: 1` on every request by default. The docstring (`anthropic.go:81-89`) claims this "fail[s] loudly rather than silently truncating," but the Anthropic SDK with `max_tokens=1` returns a valid 200 OK with a 1-token body — silent garbage, not a loud failure. The `x/provider/openai/openai.go:499-501` adapter has the inverse default (do not set `max_tokens` at all if unspecified), so the same `SummarizeStrategy.Compact` call behaves inconsistently across providers.
2. **Compaction caller.** `x/compaction/summarize.go:99` calls `s.Provider.Invoke(ctx, buf, ch)` with no options, inheriting whatever default the provider hands it. Even with a sensible default, a long-form task should size its own budget.
3. **Contract gap.** Neither adapter inspects or surfaces `stop_reason` / `finish_reason`. The anthropic adapter buffers `message_delta.Usage` (`anthropic.go:451-455`) but discards `delta.stop_reason`. The openai adapter does not inspect `chunk.Choices[0].FinishReason` at all. Truncation is structurally undetectable to the framework — the summarizer has no signal it could react to even if it wanted to.

The framework already follows the pattern of "emit typed artifacts on the streaming channel" for `Text` / `ToolCall` / `Usage` / `Reasoning` / `ReasoningSignature` / `Image` (see `artifact/artifact.go`). Adding a new artifact variant is additive; every existing type switch in the codebase (`x/conduit/tui/model.go:312+`, `x/conduit/stdio/stdio.go:134+`, `x/export/text.go:96+`, `x/export/html.go:233+`, `x/llmbytes/llmbytes.go:37+`) has a fall-through default that ignores unknown kinds.

`SummarizeStrategy.Compact` already runs a goroutine that drains the provider channel and collects `Text` / `TextDelta` artifacts (`summarize.go:73-94`). Extending it to also collect `StopReason` and react to `Length` follows the existing pattern.

## Architectural Blueprint

### Selected approach: add a canonical `StopReason` artifact, drop the broken default, and have the summarizer own its budget

1. **Add `artifact.StopReason`** — a new complete-artifact type with a typed `StopReasonKind` enum (`stop` / `length` / `tool_use` / `refusal` / `other`) that both adapters translate their provider-specific reason into. Emitted on the channel right before the buffered `Usage` artifact at the end of each stream. Lives in the `artifact/` package alongside `Text` / `Usage` / `Reasoning` — consistent with every other typed streaming signal the framework emits. The `provider/` package is for configuration types (`InvokeOption`, `ThinkingLevel`), not streaming types.
2. **Anthropic adapter** buffers the upstream `delta.stop_reason` from the `message_delta` event alongside the buffered `Usage`, translates to a canonical `StopReasonKind`, and emits both at the end of the stream. The translation table:

   | Anthropic `stop_reason` | Canonical `StopReasonKind` |
   |---|---|
   | `end_turn` | `stop` |
   | `max_tokens` | `length` |
   | `tool_use` | `tool_use` |
   | `refusal` | `refusal` |
   | `stop_sequence` and anything unknown | `other` |

3. **OpenAI adapter** extracts `chunk.Choices[0].FinishReason` from the streaming chunks, buffers the last non-empty value alongside the buffered `Usage`, and emits both at the end. Translation table:

   | OpenAI `finish_reason` | Canonical `StopReasonKind` |
   |---|---|
   | `stop` | `stop` |
   | `length` | `length` |
   | `tool_calls` | `tool_use` |
   | `content_filter` | `refusal` |
   | anything else (or empty) | (do not overwrite a previously-buffered value) |

4. **Drop the `MaxTokens: 1` default** in the anthropic adapter. Match the openai adapter's behavior: only set `params.MaxTokens` when `WithMaxTokens` is supplied. Replace the misleading "fail loudly" docstring on `WithMaxTokens` with honest guidance: the option has no default; callers should set a value appropriate to the model and the task.
5. **`SummarizeStrategy` owns its budget** — add a configurable `MaxTokens` field (default 8192) and pass it as an `InvokeOption`. Also collect `StopReason` artifacts from the provider channel; if the final one is `length`, return a sentinel `ErrTruncatedSummary` wrapped with the original turns unchanged (so callers can choose to retry, fall back, or accept).

### Why a new artifact type, not a typed return value or error

Three shapes were considered for surfacing `stop_reason` (see ideation notes):

- **Typed return value** (`(StopReason, error)` from `Invoke`) — breaking; touches every adapter and every test.
- **Typed error** from `Invoke` on truncation — conflates success-with-truncation with transport failure, and forces `errors.Is` at every call site.
- **New artifact type emitted on the channel** — additive, non-breaking, composes with the existing `OnEmit` / `FanOut` pipeline. The cost (one more artifact kind flowing past conduits) is already paid by every new artifact the framework has ever added.

### Why a canonical enum, not a string field with provider-specific values

The framework is provider-agnostic. Letting each adapter emit raw `string` values would force downstream code (compaction, analytics, exporters) to learn two vocabularies. The canonical enum is one switch statement in each adapter and one switch statement at the call site — and the call site is the same shape regardless of provider.

### Why surface as error from `SummarizeStrategy`, not auto-retry

Compaction is destructive: a truncated summary written into the buffer replaces the entire history. Silent acceptance preserves the failure mode we are fixing. Auto-retry is interesting but introduces a retry loop with no defined termination; better to expose the signal and let the application layer decide policy (retry, fall back to a different strategy, refuse to compact, etc.).

## Requirements

1. Both adapters (`anthropic` and `openai`) must emit an `artifact.StopReason` on the streaming channel, immediately before the final `Usage` artifact, for every successful stream.
2. The canonical `StopReasonKind` enum must include at minimum: `stop`, `length`, `tool_use`, `refusal`, `other`. Adding a new value is non-breaking; renaming a value is breaking.
3. The anthropic adapter must translate each of its `stop_reason` values into the canonical set per the table above. Unknown values translate to `other`.
4. The openai adapter must translate each of its `finish_reason` values into the canonical set per the table above. Unknown / empty values must not overwrite a previously-buffered value (the openai stream may emit `finish_reason: null` on intermediate deltas).
5. The anthropic adapter must drop its `MaxTokens: 1` default. `params.MaxTokens` is set only when `WithMaxTokens` is supplied. The `WithMaxTokens` docstring must no longer claim "fail loudly" and must document the absence of a default.
6. `SummarizeStrategy` must pass an explicit `WithMaxTokens` invoke option, with a default of 8192, settable via a `MaxTokens int64` field on the struct (zero value = default).
7. `SummarizeStrategy` must return a sentinel error (`ErrTruncatedSummary`, wrapped with `fmt.Errorf("...: %w", err)`) when the provider's final `StopReason` is `length`. The original turns must be returned unchanged on this path.
8. All existing tests must continue to pass without modification, except where the test was relying on the `MaxTokens: 1` default (no such tests exist; verified).
9. `go test -race ./...` must pass from the root and from each affected sub-module.
10. No external SDKs may be added to the root module's `go.mod` (per the AGENTS.md dependency rule). The `artifact/StopReason` type lives in the stdlib-only root module.

## Task Breakdown

### Task 1: Add `artifact.StopReason` and the `StopReasonKind` enum
- **Goal**: Introduce the canonical stop-reason artifact type in the root `artifact` package.
- **Dependencies**: None.
- **Files Affected**:
  - `artifact/artifact.go` (extend with new types and constants; existing patterns to follow: `Usage` struct + `Kind()` + `MarshalJSON`)
  - `artifact/artifact_test.go` (extend `TestArtifactKinds` and add a round-trip JSON test)
- **New Files**: None.
- **Interfaces**:
  - `type StopReasonKind string` with constants `StopReasonStop`, `StopReasonLength`, `StopReasonToolUse`, `StopReasonRefusal`, `StopReasonOther` (values: `"stop"`, `"length"`, `"tool_use"`, `"refusal"`, `"other"`).
  - `type StopReason struct { Reason StopReasonKind }` with `Kind() string { return "stop_reason" }` and a `MarshalJSON` that emits `{"kind":"stop_reason","reason":"<value>"}`.
- **Validation**:
  - `go test -race ./artifact/...` passes.
  - `go build ./...` from the root passes.
- **Details**:
  1. Add the `StopReasonKind` type and constants to `artifact/artifact.go`, near the other typed enums and the existing `Usage` / `ReasoningSignature` complete artifacts.
  2. Add the `StopReason` struct immediately after `Usage` (clusters end-of-stream metadata: `Usage` and `StopReason` are both emitted at the end of a stream; `StopReason` first, then `Usage`).
  3. Add a doc comment that explains the canonical set, the translation responsibility of each adapter, and the forward-compat rule (unknown upstream values become `StopReasonOther`).
  4. In `artifact_test.go`, extend `TestArtifactKinds` with `{"stop_reason", StopReason{Reason: StopReasonLength}, "stop_reason"}` and add a `TestStopReason_MarshalJSON` round-trip test for each constant value. Update the compile-time interface satisfaction block at the top of the file with `var _ Artifact = StopReason{}`.

### Task 2: Anthropic adapter — extract `stop_reason`, emit `StopReason`
- **Goal**: Translate the upstream `delta.stop_reason` from the `message_delta` SSE event into a canonical `StopReason`, buffer it alongside the buffered `Usage`, and emit both at the end of the stream.
- **Dependencies**: Task 1 (uses `artifact.StopReason` and the canonical enum).
- **Files Affected**:
  - `x/provider/anthropic/anthropic.go` (modify `dispatchEvent` `MessageDeltaEvent` case; modify the post-loop emission block at `anthropic.go:401-417`; possibly add a small `pendingStopReason *artifact.StopReasonKind` field on the pending usage carrier or a sibling field on the function)
  - `x/provider/anthropic/anthropic_test.go` (add tests for each canonical mapping; update existing tests that assert on artifact ordering)
- **New Files**: None.
- **Interfaces**: New `artifact.StopReason` flows on the channel. No change to the `Provider.Invoke` signature.
- **Validation**:
  - `go test -race ./x/provider/anthropic/...` passes.
  - New test `TestProviderInvoke_EmitsStopReason_Length` asserts that a stream with `message_delta` carrying `stop_reason: "max_tokens"` produces an `artifact.StopReason{Reason: StopReasonLength}` immediately before the final `Usage`.
  - New test `TestProviderInvoke_EmitsStopReason_EndTurn` asserts the `end_turn → stop` mapping.
  - New test `TestProviderInvoke_EmitsStopReason_ToolUse` asserts the `tool_use → tool_use` mapping.
  - New test `TestProviderInvoke_EmitsStopReason_UnknownMapsToOther` asserts the `stop_sequence → other` mapping.
  - All existing tests (`TestProviderInvoke_StreamsThinking`, `TestProviderInvoke_StreamsMixedTextAndThinking`, `TestProviderInvoke_StreamsToolCall`, etc.) continue to pass — their `require.Len(t, got, N)` assertions grow by 1 if the test expected only the buffered `Usage` as the trailing artifact, so the test must be updated to expect the new `StopReason` before the `Usage`.
- **Details**:
  1. In `dispatchEvent`'s `MessageDeltaEvent` case (`anthropic.go:451-455`), read `ev.Delta.StopReason` (verify the SDK field name; it is `anthropic.MessageDeltaEvent.Delta.StopReason` of type `anthropic.StopReason`). Translate to `artifact.StopReasonKind` via a small local function `translateStopReason(anthropic.StopReason) artifact.StopReasonKind`. Buffer into a new local `pendingStopReason artifact.StopReasonKind` parameter (or extend the existing `pendingUsage` pattern by passing both as function parameters through `dispatchEvent`).
  2. After the `MessageStopEvent` break, in the block that emits `pendingUsage` (`anthropic.go:409-417`), emit the buffered `StopReason` first (if non-zero, i.e. the SDK actually reported a reason), then the `Usage`. Use the same `sendOrCancel` pattern.
  3. The `translateStopReason` mapping:
     ```go
     func translateStopReason(s anthropic.StopReason) artifact.StopReasonKind {
         switch s {
         case anthropic.StopReasonEndTurn:       return artifact.StopReasonStop
         case anthropic.StopReasonMaxTokens:     return artifact.StopReasonLength
         case anthropic.StopReasonToolUse:       return artifact.StopReasonToolUse
         case anthropic.StopReasonRefusal:       return artifact.StopReasonRefusal
         default:                                return artifact.StopReasonOther
         }
     }
     ```
     Verify the SDK constant names against `github.com/anthropics/anthropic-sdk-go` during implementation; the Go SDK names them as `anthropic.StopReasonEndTurn`, `anthropic.StopReasonMaxTokens`, `anthropic.StopReasonToolUse`, `anthropic.StopReasonRefusal` (and possibly `anthropic.StopReasonStopSequence`).
  4. In tests, update existing `require.Len(t, got, N)` calls that expected `Usage` to be the last artifact. The new expected ordering is: deltas → (optional reasoning signature) → `StopReason` → `Usage`. The simplest fix: assert `got[len(got)-1].Kind() == "usage"` and `got[len(got)-2].Kind() == "stop_reason"` at the end of each existing test, rather than hardcoding lengths.
  5. The `triggerRequest` test helper (`anthropic_test.go:115-135`) is not affected — it calls `p.client.Messages.New` directly, bypassing `Invoke`.

### Task 3: OpenAI adapter — extract `finish_reason`, emit `StopReason`
- **Goal**: Translate the upstream `chunk.Choices[0].FinishReason` from the streaming chunks into a canonical `StopReason`, buffer the last non-empty value alongside the buffered `Usage`, and emit both at the end of the stream.
- **Dependencies**: Task 1.
- **Files Affected**:
  - `x/provider/openai/openai.go` (modify the streaming loop at `openai.go:565-580` to capture `chunk.Choices[0].FinishReason`; modify the final emission block at `openai.go:563-581` to emit `StopReason` before `Usage`)
  - `x/provider/openai/openai_test.go` (add tests for each canonical mapping; update existing tests that assert on artifact ordering)
- **New Files**: None.
- **Interfaces**: New `artifact.StopReason` flows on the channel. No change to the `Provider.Invoke` signature.
- **Validation**:
  - `go test -race ./x/provider/openai/...` passes.
  - New test `TestProviderInvoke_EmitsStopReason_Length` asserts the `finish_reason: "length" → StopReasonLength` mapping.
  - New test `TestProviderInvoke_EmitsStopReason_Stop` asserts the `finish_reason: "stop" → StopReasonStop` mapping.
  - New test `TestProviderInvoke_EmitsStopReason_ToolCalls` asserts the `finish_reason: "tool_calls" → StopReasonToolUse` mapping.
  - New test `TestProviderInvoke_EmitsStopReason_ContentFilter` asserts the `finish_reason: "content_filter" → StopReasonRefusal` mapping.
  - New test `TestProviderInvoke_StopReason_NotOverwrittenByNull` asserts that a stream emitting `finish_reason: null` on intermediate deltas does not clobber a previously-buffered reason.
  - All existing tests continue to pass with updated length assertions (same pattern as Task 2).
- **Details**:
  1. In the `for stream.Next()` loop, add a `pendingStopReason` local variable (or a small struct `pending { reason artifact.StopReasonKind; usage *artifact.Usage }`). On every chunk with `len(chunk.Choices) > 0`, read `chunk.Choices[0].FinishReason` and translate. Only update the buffered reason when the translated value is not the zero value (`""`) AND the upstream value is non-empty (because OpenAI sends `finish_reason: null` on intermediate deltas, the SDK will likely return an empty / zero `StopReason` for those).
  2. The `translateFinishReason` mapping:
     ```go
     func translateFinishReason(r openai.ChatCompletionChunkChoiceFinishReason) artifact.StopReasonKind {
         switch r {
         case openai.ChatCompletionChunkChoiceFinishReasonStop:          return artifact.StopReasonStop
         case openai.ChatCompletionChunkChoiceFinishReasonLength:       return artifact.StopReasonLength
         case openai.ChatCompletionChunkChoiceFinishReasonToolCalls:    return artifact.StopReasonToolUse
         case openai.ChatCompletionChunkChoiceFinishReasonContentFilter: return artifact.StopReasonRefusal
         default:                                                        return artifact.StopReasonOther
         }
     }
     ```
     Verify the exact SDK constant names against `github.com/openai/openai-go` during implementation. The Go SDK exposes `ChatCompletionChunkChoiceFinishReason` as a string-typed enum.
  3. In the post-loop emission block, emit the buffered `StopReason` first, then the buffered `Usage` (if non-nil). The current code emits `Usage` only when `chunk.Usage.TotalTokens > 0`; preserve that gate for the `Usage` emission. The `StopReason` emission is unconditional — if the stream produced a non-empty reason, emit it.
  4. In tests, the existing `TestProviderInvoke_UsageChunk` test (`openai_test.go:274-296`) currently asserts `require.Len(t, artifacts, 1)` and that `artifacts[0]` is `Usage`. With the new behavior, the stream produces a `StopReason` first, then `Usage` — so the test should be updated to expect `len == 2` and assert the ordering.

### Task 4: Drop the Anthropic `MaxTokens: 1` default
- **Goal**: Make the anthropic adapter's default behavior match the openai adapter's — only set `params.MaxTokens` when the caller supplies `WithMaxTokens`. Fix the misleading docstring on `WithMaxTokens`.
- **Dependencies**: None. Independent of Tasks 1-3.
- **Files Affected**:
  - `x/provider/anthropic/anthropic.go` (delete the `MaxTokens: 1` initializer at line 363; update the docstring on `WithMaxTokens` at lines 81-89)
- **New Files**: None.
- **Interfaces**: `WithMaxTokens` is now required for any meaningful response. The docstring documents this honestly.
- **Validation**:
  - `go test -race ./x/provider/anthropic/...` passes.
  - All existing tests continue to pass — none of them rely on the 1-token default (the only `MaxTokens: 1` reference in the test file is in the `triggerRequest` helper at `anthropic_test.go:129`, which sets the value directly on `MessageNewParams` for a non-streaming test fixture, bypassing the `Invoke` flow).
  - The updated docstring on `WithMaxTokens` should state: callers are responsible for setting a value appropriate to the model and the task. There is no default; omitting this option means the SDK / model default applies (the same as the openai adapter's behavior).
- **Details**:
  1. Delete the `MaxTokens: 1, // SDK minimum; overridden by per-invocation option below if set.` initializer at `anthropic.go:363`.
  2. The subsequent `if inv.maxTokensSet { params.MaxTokens = inv.maxTokens }` block already does the right thing — it only sets the value when the caller supplied the option. With the initializer removed, the zero value of `params.MaxTokens` is `0`, which the Anthropic SDK will reject. This is the intended behavior: callers must opt in.
  3. Update the `WithMaxTokens` docstring at `anthropic.go:81-89` to remove the "fail loudly rather than silently truncating on an arbitrary cap" sentence and replace with: `Callers are responsible for picking a value appropriate to the model and the task; different Anthropic models have different output caps. There is no default — omitting this option leaves the Anthropic SDK / model default in effect.`
  4. If a test relies on `max_tokens` being unset and producing a usable response, that test should be updated to pass `WithMaxTokens(4096)` (or similar) explicitly. (Spot-check during implementation; expected to be a no-op based on the existing test fixtures.)

### Task 5: `SummarizeStrategy` — own its budget, error on `Length`
- **Goal**: The strategy explicitly requests a long-form budget (default 8192, configurable) and surfaces truncation as a sentinel error from `Compact`.
- **Dependencies**: Task 1 (uses `artifact.StopReason`). Tasks 2 and 3 are not strictly required for this task to compile and pass tests in isolation — the strategy's mock-based test can emit `StopReason` directly. The end-to-end behavior only materializes once the adapters also emit `StopReason`, but the strategy is independently committable.
- **Files Affected**:
  - `x/compaction/summarize.go` (add `MaxTokens` field; add `defaultSummarizeMaxTokens` const = 8192; modify `Compact` to pass `WithMaxTokens` as an invoke option; modify the channel-drain goroutine to also collect `StopReason`; check the final reason after the channel closes; return `ErrTruncatedSummary` if `length`)
  - `x/compaction/summarize_test.go` (add `receivedOpts` capture to the mock; add a test that verifies `WithMaxTokens` is passed with the default; add a test that verifies a custom `MaxTokens` is forwarded; add a test that verifies `ErrTruncatedSummary` is returned on `StopReason{Length}`; add a test that verifies non-`Length` reasons do not error)
  - `x/compaction/doc.go` (document the new `MaxTokens` field, the new sentinel error, and the updated `Compact` error behavior)
- **New Files**: None.
- **Interfaces**:
  - `type SummarizeStrategy` gains a `MaxTokens int64` field. Zero value → `defaultSummarizeMaxTokens` (8192). Non-zero value → forwarded as `WithMaxTokens(n)`.
  - `var ErrTruncatedSummary = errors.New("summarization produced truncated result")` — sentinel for `errors.Is` checks. Returned wrapped via `fmt.Errorf("...: %w", ErrTruncatedSummary)`.
  - `Compact` signature unchanged: `func (s SummarizeStrategy) Compact(ctx context.Context, turns []state.Turn) ([]state.Turn, error)`. The error return may now be `ErrTruncatedSummary` (wrapped) in addition to the existing provider-error and nil-provider error cases.
- **Validation**:
  - `go test -race ./x/compaction/...` passes.
  - New test `TestSummarizeStrategy_AppliesDefaultMaxTokens` asserts that when `SummarizeStrategy{}` is constructed with the zero-value `MaxTokens`, the captured `receivedOpts` includes a `WithMaxTokens(8192)` option.
  - New test `TestSummarizeStrategy_AppliesCustomMaxTokens` asserts that a `SummarizeStrategy{MaxTokens: 4096}` forwards `WithMaxTokens(4096)`.
  - New test `TestSummarizeStrategy_ZeroMaxTokensFallsBackToDefault` asserts that an explicit `MaxTokens: 0` also falls back to the default (defensive — zero is the unset value).
  - New test `TestSummarizeStrategy_TruncatedResultReturnsError` configures the mock to emit a `StopReason{Reason: StopReasonLength}` after a `Text` artifact; asserts the returned error wraps `ErrTruncatedSummary` (use `errors.Is`) and that the original turns slice is returned.
  - New test `TestSummarizeStrategy_StopReasonNonLengthDoesNotError` configures the mock to emit a `StopReason{Reason: StopReasonStop}`; asserts the result is the normal single-turn summary and the error is nil.
  - New test `TestSummarizeStrategy_StopReasonToolUseDoesNotError` configures the mock to emit a `StopReason{Reason: StopReasonToolUse}`; asserts no error (the strategy is not a tool-caller and a tool_use reason on the summarization call is an oddity but not a truncation).
  - All existing tests in `summarize_test.go` continue to pass — the mock now needs a `receivedOpts` field to make the new tests possible, but existing tests that do not assert on options are unaffected (the mock is shared; the new field is zero-valued by default).
- **Details**:
  1. In `summarize.go`, add the `defaultSummarizeMaxTokens` constant near the existing `defaultPrompt` constant. The naming mirrors the existing `defaultPrompt` precedent.
  2. Add `MaxTokens int64` to the `SummarizeStrategy` struct, with a doc comment that explains the defaulting behavior (zero = 8192).
  3. Resolve the effective `maxTokens` at the start of `Compact`: `n := s.MaxTokens; if n <= 0 { n = defaultSummarizeMaxTokens }`. Build an `opts := []provider.InvokeOption{anthropic.WithMaxTokens(n)}` (or openai's equivalent — see "open question" below) and pass it to `s.Provider.Invoke(ctx, buf, ch, opts...)`.
  4. Extend the channel-drain goroutine to also collect `StopReason`:
     ```go
     var lastStopReason artifact.StopReasonKind
     // ... in the for-range loop ...
     case artifact.StopReason:
         lastStopReason = a.Reason
     ```
     Then, after `wg.Wait()`, if `lastStopReason == artifact.StopReasonLength`, return `originalTurns, fmt.Errorf("...: %w", ErrTruncatedSummary)` — using the `originalTurns` capture from before the strategy ran (the same pattern `MaybeCompact` uses for the error path).
  5. Add `ErrTruncatedSummary` to `summarize.go` (or a new file `errors.go` in the same package if you prefer to keep concerns separate; either is fine). The sentinel must be exported.
  6. In `summarize_test.go`, add a `receivedOpts []provider.InvokeOption` field to `mockProvider`, and append `opts...` in `Invoke`. Existing tests that do not assert on `receivedOpts` are unaffected. The new tests for option forwarding assert `len(m.receivedOpts) > 0` and type-assert the first option to `anthropic.WithMaxTokens` (or openai's, depending on the import path — see "open question" below).
  7. Update `x/compaction/doc.go` to:
     - Document the new `MaxTokens` field.
     - Document `ErrTruncatedSummary` and the new failure mode.
     - Update the wiring example to show that callers can set `MaxTokens` explicitly.
  8. **Open question — which `WithMaxTokens` to import?** The summarizer lives in `x/compaction/`, which is its own Go module (`x/compaction/go.mod`). It does not depend on `x/provider/anthropic` or `x/provider/openai`. To pass `WithMaxTokens(n)`, it would need to import one of them, or it would need a `provider.WithMaxTokens(n)` function in the root `provider/` package. The latter is the right answer — it's provider-agnostic and matches the existing `provider.WithTools` and `provider.WithModel` precedent. Add `type MaxTokensOption struct { N int64 }`, `func (MaxTokensOption) IsInvokeOption() {}`, and `func WithMaxTokens(n int64) InvokeOption { return MaxTokensOption{N: n} }` to `provider/provider.go`. Each adapter (anthropic, openai) recognizes `provider.MaxTokensOption` in its `applyInvokeOptions` and translates it to its own wire format. (This is a small refactor — see the "open question" subsection below for the implementation approach.) Alternatively, the simpler path is to have the summarizer type-assert on the provider's concrete type: `if ap, ok := s.Provider.(anthropicProvider); ok { opts = append(opts, ap.WithMaxTokens(n)) }`. The first approach is cleaner. Decision flag below.

## Dependency Graph

```
Task 1 (artifact.StopReason)
   ├─→ Task 2 (anthropic emits StopReason)
   ├─→ Task 3 (openai emits StopReason)
   └─→ Task 5 (SummarizeStrategy owns budget, errors on Length)
Task 4 (drop MaxTokens:1 default)   ← independent of Tasks 1-3
```

Tasks 2, 3, 4, 5 are all parallelizable after Task 1 completes. Task 4 has no dependency on Task 1. The end-to-end behavior — "compaction surfaces a useful error on truncation" — only materializes after Tasks 1, 2, 3, 4, 5 all land; each task is independently committable.

## Open Question — Provider-Agnostic `WithMaxTokens` in the Root `provider` Package

Task 5's description has two viable implementations of "the summarizer passes an explicit `WithMaxTokens`":

**Option A (cleaner):** Add `provider.WithMaxTokens(n int64) InvokeOption` in the root `provider/provider.go`. Each adapter's `applyInvokeOptions` recognizes `provider.MaxTokensOption` and translates it to the adapter's wire format. The summarizer imports only `provider` (already a dependency), not any concrete adapter.

**Option B (more local):** Have the summarizer type-assert on the provider's concrete type to call its package-local `WithMaxTokens`. Two implementations of the type assertion (anthropic, openai), one for any future provider, ugly.

**Option C (simplest, narrow):** The summarizer calls `s.Provider.Invoke(ctx, buf, ch, anthropic.WithMaxTokens(n))` — but `x/compaction` would gain a dependency on `x/provider/anthropic`, breaking the framework's provider-agnosticism for the compaction package.

**Recommendation: Option A.** It mirrors the existing `provider.WithTools` / `provider.WithModel` pattern and keeps `x/compaction` provider-agnostic. Cost: a small addition to `provider/provider.go` and a 2-3 line change in each adapter's `applyInvokeOptions`. The plan budgets this into Task 5 (or, if the implementer prefers, a small Task 0 / Task 1.5 — but it's small enough to live inside Task 5).

This decision can be made by the implementer without further consultation. Flag it in the commit message of Task 5 so it is visible in the diff history.

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| `go.work.sum` requires updating when sub-modules change | Low | High | Run `go mod tidy` in each affected sub-module (anthropic, openai, compaction) and `go work sync` from the root after each task. |
| Existing anthropic tests that count emitted artifacts break when the new `StopReason` is added | Low | High | Update affected `require.Len(t, got, N)` assertions to `N+1` and assert the second-to-last is `stop_reason`. Document this in Task 2's Details. |
| `openai.ChatCompletionChunkChoiceFinishReason` constant names differ from the plan's guess | Low | Medium | Verify against the SDK's source during implementation; the plan describes the *mapping* and the *behavior*, not the literal Go constant name. |
| The anthropic `message_delta` event's `delta.stop_reason` field is named differently in the SDK | Low | Low | Verify against the SDK's source during implementation. The plan describes the *behavior*; the literal field access can be adjusted. |
| The openai stream's intermediate `finish_reason: null` chunks overwrite the buffered reason | Medium | Medium | Guard the update with a "non-zero value" check; covered by the new test `TestProviderInvoke_StopReason_NotOverwrittenByNull` in Task 3. |
| Removing the `MaxTokens: 1` default causes an unrelated test to start failing (an SDK request that relied on max_tokens=0 for cache priming) | Low | Low | Spot-check existing tests during Task 4. If a test depends on the default, update it to pass an explicit `WithMaxTokens`. No such test was found during discovery. |
| The summarizer's `ErrTruncatedSummary` collides with an existing error name in the package | Low | Low | `summarize.go` has no other exported errors; safe. |
| The new `provider.WithMaxTokens` collides with the existing per-adapter `WithMaxTokens` exported names | Low | High (cosmetic) | The collision is at the package level (e.g. `anthropic.WithMaxTokens` vs `provider.WithMaxTokens`), not within a single package. Call sites already disambiguate with package qualifiers (the existing `provider.WithModel` / `openai.WithModel` precedent). |
| Auto-retry on truncation is desired by users but not implemented | Low | High (eventually) | Documented in the Requirements as out of scope. Callers can wrap the `SummarizeStrategy` in a retry strategy if they want auto-retry. The decision is reversible. |
| A future adapter (e.g. Gemini) emits a `stop_reason` vocabulary not covered by the canonical set | Low | Low | The `other` constant is the catch-all; the adapter can map unknown values to it. Adding new canonical values is non-breaking. |

## Validation Criteria

- [ ] `artifact.StopReason` and `artifact.StopReasonKind` exist with the constants `StopReasonStop`, `StopReasonLength`, `StopReasonToolUse`, `StopReasonRefusal`, `StopReasonOther`.
- [ ] `artifact.StopReason{}.Kind() == "stop_reason"`.
- [ ] `artifact.StopReason{Reason: StopReasonLength}.MarshalJSON()` produces `{"kind":"stop_reason","reason":"length"}`.
- [ ] The anthropic adapter emits a `StopReason` artifact on the channel immediately before the final `Usage` artifact for `end_turn`, `max_tokens`, `tool_use`, `refusal`, and unknown (`stop_sequence` or otherwise) upstream reasons.
- [ ] The openai adapter emits a `StopReason` artifact on the channel immediately before the final `Usage` artifact for `stop`, `length`, `tool_calls`, `content_filter`, and unknown upstream reasons.
- [ ] The openai adapter does not overwrite a previously-buffered `StopReason` with the zero / empty value when an intermediate chunk reports `finish_reason: null`.
- [ ] The anthropic adapter does not set `params.MaxTokens` when `WithMaxTokens` is not supplied (no default). The `WithMaxTokens` docstring no longer claims "fail loudly."
- [ ] `provider.WithMaxTokens(n int64) InvokeOption` exists in the root `provider` package and is recognized by both the anthropic and openai adapters.
- [ ] `SummarizeStrategy{MaxTokens: 0}.Compact(...)` passes `WithMaxTokens(8192)` to the provider.
- [ ] `SummarizeStrategy{MaxTokens: 4096}.Compact(...)` passes `WithMaxTokens(4096)` to the provider.
- [ ] `SummarizeStrategy.Compact(...)` returns an error wrapping `ErrTruncatedSummary` (verifiable via `errors.Is`) when the provider's final `StopReason` is `length`, and the original turns are returned unchanged.
- [ ] `SummarizeStrategy.Compact(...)` does not return `ErrTruncatedSummary` for `StopReason` values other than `length`.
- [ ] `go test -race ./...` passes from the root and from each affected sub-module (`artifact`, `provider`, `x/provider/anthropic`, `x/provider/openai`, `x/compaction`).
- [ ] `go build ./...` passes from the root and from each affected sub-module.
- [ ] `go.mod` files in the root module and in `x/compaction/go.mod`, `x/provider/anthropic/go.mod`, `x/provider/openai/go.mod` reflect any dependency changes; no new external dependencies are added to the root module.
