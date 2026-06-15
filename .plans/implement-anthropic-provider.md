# Plan: Implement Anthropic Provider

## Objective
Implement a new provider package `x/provider/anthropic` that supports the Anthropic Messages API. This provider will be wire-compatible with both Anthropic native and OpenRouter's `/api/v1/messages` endpoint, ensuring that reasoning traces (thinking blocks) are correctly streamed as `artifact.ReasoningDelta` and persisted/replayed across conversation turns.

## Context
Currently, `ore` only has an OpenAI-compatible provider. While OpenRouter provides a `/chat/completions` translation, it often drops or mis-formats reasoning content for non-Claude models. The canonical way to handle reasoning-capable models on OpenRouter and Anthropic is via the `/api/v1/messages` API.

Findings:
- Repository Topology: Providers live under `x/provider/`. `x/provider/openai` serves as the primary reference.
- Core Interfaces: The provider must implement `provider.Provider` and emit `artifact.Artifact` (TextDelta, ReasoningDelta, ToolCallDelta, Usage).
- Project Conventions: While `AGENTS.md` generally advises against external SDKs, packages under `x/` may utilize separate `go.mod` files to isolate dependencies and keep the core framework light.

## Architectural Blueprint
The `x/provider/anthropic` package will be a standalone adapter using the official `github.com/anthropics/anthropic-sdk-go` SDK. It will handle the translation between `ore`'s `state.State` and the Anthropic Messages API.

**Key Components:**
- **Configuration**: Functional options for API key, model, base URL, and thinking configuration.
- **Request Serializer**: Leverages SDK types to convert `state.State` into messages, specifically handling the injection of `thinking` and `redacted_thinking` blocks for multi-turn reasoning replay.
- **Stream Processor**: Consumes the SDK's streaming response and emits `ore` artifacts in arrival order.
- **Usage Tracker**: Extracts token counts, including specific `thinking_tokens`.

**Tree-of-Thought Deliberation:**
- *Path A: Use `anthropics/anthropic-sdk-go`*: Faster to implement, more robust request/response types, and lower risk of parsing bugs.
- *Path B: Manual `net/http` implementation*: Maximum control, zero dependencies, but higher implementation cost and risk of SSE parsing errors.
- *Selected Path*: Path A. By using a separate `go.mod` within `x/provider/anthropic`, we gain the productivity of the SDK without polluting the core project's dependency graph.

## Requirements
- [ ] Implement `provider.Provider` interface.
- [ ] Support `https://api.anthropic.com` (native) and `https://openrouter.ai/api/v1` (mirror).
- [ ] Stream `thinking_delta` as `artifact.ReasoningDelta`.
- [ ] Stream `text_delta` as `artifact.TextDelta`.
- [ ] Stream `input_json_delta` as `artifact.ToolCallDelta`.
- [ ] Extract `thinking_tokens` from usage and report in `artifact.Usage`.
- [ ] Support multi-turn replay of reasoning by including previous `thinking` blocks in the request.
- [ ] Preserve `redacted_thinking` signatures for replay.

## Task Breakdown

### Task 1: Initialize Package and Dependencies
- **Goal**: Establish the package structure and isolate dependencies.
- **Dependencies**: None.
- **Files Affected**: None.
- **New Files**: `x/provider/anthropic/anthropic.go`, `x/provider/anthropic/doc.go`, `x/provider/anthropic/go.mod`.
- **Interfaces**: 
    - `Provider` struct.
    - `Option` func.
    - `WithAPIKey`, `WithModel`, `WithBaseURL`, `WithAnthropicVersion`, `WithThinking`.
- **Validation**: `go mod tidy` in `x/provider/anthropic` succeeds.
- **Details**: Create the directory, run `go mod init`, and add `github.com/anthropics/anthropic-sdk-go`. Implement the basic constructor and configuration logic.

### Task 2: Implement State Serialization (Request Builder)
- **Goal**: Convert `ore` state into Anthropic SDK message types.
- **Dependencies**: Task 1.
- **Files Affected**: `x/provider/anthropic/anthropic.go`.
- **Interfaces**: `serializeMessages(state.State) []anthropic.Message`.
- **Validation**: Unit test verifying that `state.RoleAssistant` turns with `Reasoning` artifacts are converted to `thinking` blocks in the request.
- **Details**: Walk the `state.State` and map roles. Use the SDK's types to ensure correct formatting for reasoning content to enable model continuity.

### Task 3: Implement Stream Processing
- **Goal**: Translate the SDK's streaming response into `ore` artifacts.
- **Dependencies**: Task 1.
- **Files Affected**: `x/provider/anthropic/anthropic.go`.
- **Interfaces**: `Invoke(ctx, state, ch, opts) error`.
- **Validation**: Mock server test verifying that thinking deltas produce `ReasoningDelta` and text deltas produce `TextDelta`.
- **Details**: Implement the `Invoke` method. Use the SDK's streaming client. Iterate through the stream and emit corresponding `ore` artifact types into the channel immediately upon receipt.

### Task 4: Implement Tool Call Handling
- **Goal**: Support tool use via SDK tool-use blocks.
- **Dependencies**: Task 3.
- **Files Affected**: `x/provider/anthropic/anthropic.go`.
- **Interfaces**: Integration with `artifact.ToolCallDelta`.
- **Validation**: Test verifying that tool calls are correctly parsed and emitted as `ToolCallDelta` fragments.
- **Details**: Map SDK tool-use events to `ore`'s tool call artifacts.

### Task 5: Implement Usage and Thinking Token Extraction
- **Goal**: Correctly report token usage, specifically distinguishing thinking tokens.
- **Dependencies**: Task 3.
- **Files Affected**: `x/provider/anthropic/anthropic.go`.
- **Interfaces**: `artifact.Usage`.
- **Validation**: Test verifying that `thinking_tokens` from the SDK's usage response is reflected in the final `artifact.Usage` artifact.
- **Details**: Extract token counts from the final message or usage event.

### Task 6: Implement Reasoning Signature Persistence (Redacted Thinking)
- **Goal**: Ensure `redacted_thinking` blocks are preserved for multi-turn replay.
- **Dependencies**: Task 2, Task 3.
- **Files Affected**: `x/provider/anthropic/anthropic.go`, potentially `artifact/artifact.go` (if extension is needed).
- **Interfaces**: Determine where to store the signature (e.g., as a field in `artifact.Usage` or a new artifact type).
- **Validation**: Test verifying that a `redacted_thinking` block received in turn N is sent back in the request for turn N+1.
- **Details**: Determine the most non-intrusive way to persist the signature so the `serializeMessages` function can include it in subsequent requests.

### Task 7: Integration Testing with OpenRouter/Anthropic
- **Goal**: End-to-end validation against a real API.
- **Dependencies**: Task 1-6.
- **Files Affected**: `x/provider/anthropic/anthropic_test.go`.
- **Validation**: Network test (env-gated) using a real API key and a reasoning model (e.g., `claude-3-5-sonnet` or `minimax/minimax-m3` via OR) asserting that thinking blocks are received.
- **Details**: Create a test that performs a full round-trip and asserts the presence of `Reasoning` artifacts in the resulting state.

## Dependency Graph
- Task 1 $\rightarrow$ Task 2
- Task 1 $\rightarrow$ Task 3
- Task 3 $\rightarrow$ Task 4
- Task 3 $\rightarrow$ Task 5
- (Task 2, Task 3) $\rightarrow$ Task 6
- (Task 1, 2, 3, 4, 5, 6) $\rightarrow$ Task 7

## Risks & Mitigations
| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| SDK Abstraction Leak | Medium | Low | Verify that SDK streaming events are emitted immediately to the channel without internal buffering. |
| Signature Storage Intrusion | Low | Medium | Evaluate adding a `Metadata` map to `artifact.Usage` or a specific `ReasoningSignature` artifact. |
| API Version Mismatches | Medium | Low | Default to current SDK version and make the API version configurable if the SDK supports it. |

## Validation Criteria
- [ ] `x/provider/anthropic` implements `provider.Provider`.
- [ ] SDK stream `thinking_delta` $\rightarrow$ `artifact.ReasoningDelta`.
- [ ] SDK stream `text_delta` $\rightarrow$ `artifact.TextDelta`.
- [ ] `thinking_tokens` reported in `artifact.Usage`.
- [ ] Prior turn `thinking` blocks are included in subsequent requests.
- [ ] `redacted_thinking` signatures are preserved and replayed.
- [ ] All unit and integration tests pass.
