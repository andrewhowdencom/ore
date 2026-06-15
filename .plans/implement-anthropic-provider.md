# Plan: Implement Anthropic Provider

## Objective
Implement a new provider package `x/provider/anthropic` that supports the Anthropic Messages API. This provider will be wire-compatible with both Anthropic native and OpenRouter's `/api/v1/messages` endpoint, ensuring that reasoning traces (thinking blocks) are correctly streamed as `artifact.ReasoningDelta` and persisted/replayed across conversation turns.

## Context
Currently, `ore` only has an OpenAI-compatible provider. While OpenRouter provides a `/chat/completions` translation, it often drops or mis-formats reasoning content for non-Claude models. The canonical way to handle reasoning-capable models on OpenRouter and Anthropic is via the `/api/v1/messages` API.

Findings:
- Repository Topology: Providers live under `x/provider/`. `x/provider/openai` serves as the primary reference.
- Core Interfaces: The provider must implement `provider.Provider` and emit `artifact.Artifact` (TextDelta, ReasoningDelta, ToolCallDelta, Usage).
- Project Conventions: `AGENTS.md` advises against external SDKs in providers to keep them minimal. The implementation should use `net/http` and `encoding/json`.

## Architectural Blueprint
The `x/provider/anthropic` package will be a standalone adapter. It will handle the translation between `ore`'s `state.State` and the Anthropic Messages API request/response format.

**Key Components:**
- **Configuration**: Functional options for API key, model, base URL, and thinking configuration.
- **Request Serializer**: Converts `state.State` into the Anthropic `messages` array, specifically handling the injection of `thinking` and `redacted_thinking` blocks for multi-turn reasoning replay.
- **SSE Stream Parser**: A state-machine based parser that consumes the Anthropic SSE stream and emits `ore` artifacts in arrival order.
- **Usage Tracker**: Extracts token counts, including specific `thinking_tokens`.

**Tree-of-Thought Deliberation:**
- *Path A: Use `anthropics/anthropic-sdk-go`*: Faster to implement but violates the "minimal dependencies" rule in `AGENTS.md`.
- *Path B: Manual `net/http` implementation*: Aligns with project philosophy, gives full control over SSE parsing, and avoids SDK bloat.
- *Selected Path*: Path B. The Anthropic Messages API is a straightforward REST/SSE interface.

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

### Task 1: Define Provider Structure and Configuration
- **Goal**: Establish the package structure and configuration options.
- **Dependencies**: None.
- **Files Affected**: None.
- **New Files**: `x/provider/anthropic/anthropic.go`, `x/provider/anthropic/doc.go`.
- **Interfaces**: 
    - `Provider` struct.
    - `Option` func.
    - `WithAPIKey`, `WithModel`, `WithBaseURL`, `WithAnthropicVersion`, `WithThinking`.
- **Validation**: `go build ./x/provider/anthropic/...` passes.
- **Details**: Implement the basic constructor and configuration logic following the pattern in `x/provider/openai`.

### Task 2: Implement State Serialization (Request Builder)
- **Goal**: Convert `ore` state into the Anthropic Messages API request body.
- **Dependencies**: Task 1.
- **Files Affected**: `x/provider/anthropic/anthropic.go`.
- **Interfaces**: `serializeMessages(state.State) []Message`.
- **Validation**: Unit test verifying that `state.RoleAssistant` turns with `Reasoning` artifacts are converted to `thinking` blocks in the request.
- **Details**: Walk the `state.State` and map roles. Ensure that reasoning content is properly emitted as `thinking` blocks to enable model continuity.

### Task 3: Implement SSE Stream Parser
- **Goal**: Translate the Anthropic SSE event stream into `ore` artifacts.
- **Dependencies**: Task 1.
- **Files Affected**: `x/provider/anthropic/anthropic.go`.
- **Interfaces**: `Invoke(ctx, state, ch, opts) error`.
- **Validation**: Mock SSE server test verifying that `content_block_delta` (thinking) produces `ReasoningDelta` and `content_block_delta` (text) produces `TextDelta`.
- **Details**: Implement the `Invoke` method. Use `net/http` to start a streaming request. Parse SSE events (`message_start`, `content_block_delta`, `message_delta`, etc.) and push corresponding `artifact` types into the channel.

### Task 4: Implement Tool Call Handling
- **Goal**: Support tool use via `input_json_delta`.
- **Dependencies**: Task 3.
- **Files Affected**: `x/provider/anthropic/anthropic.go`.
- **Interfaces**: Integration with `artifact.ToolCallDelta`.
- **Validation**: Test verifying that tool calls are correctly parsed and emitted as `ToolCallDelta` fragments.
- **Details**: Handle `content_block_start` with type `tool_use` and subsequent `input_json_delta` events.

### Task 5: Implement Usage and Thinking Token Extraction
- **Goal**: Correctly report token usage, specifically distinguishing thinking tokens.
- **Dependencies**: Task 3.
- **Files Affected**: `x/provider/anthropic/anthropic.go`.
- **Interfaces**: `artifact.Usage`.
- **Validation**: Test verifying that `usage.output_tokens_details.thinking_tokens` from the API is reflected in the final `artifact.Usage` artifact.
- **Details**: Parse the `message_delta` or final `message` event to extract token counts.

### Task 6: Implement Reasoning Signature Persistence (Redacted Thinking)
- **Goal**: Ensure `redacted_thinking` blocks are preserved for multi-turn replay.
- **Dependencies**: Task 2, Task 3.
- **Files Affected**: `x/provider/anthropic/anthropic.go`, potentially `artifact/artifact.go` (if extension is needed).
- **Interfaces**: Determine where to store the signature (e.g., as a field in `artifact.Usage` or a new artifact type).
- **Validation**: Test verifying that a `redacted_thinking` block received in turn N is sent back in the request for turn N+1.
- **Details**: This is the most complex part of the replay. The implementer must decide on a non-intrusive way to store the signature so the `serializeMessages` function can retrieve it.

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
| SSE Parsing Complexity | Medium | Medium | Use a well-tested SSE parsing pattern or a small, focused helper. |
| Signature Storage Intrusion | Low | Medium | Evaluate adding a `Metadata` map to `artifact.Usage` or a specific `ReasoningSignature` artifact. |
| API Version Mismatches | Medium | Low | Default to `2023-06-01` and make it configurable via `WithAnthropicVersion`. |

## Validation Criteria
- [ ] `x/provider/anthropic` implements `provider.Provider`.
- [ ] SSE stream `thinking_delta` $\rightarrow$ `artifact.ReasoningDelta`.
- [ ] SSE stream `text_delta` $\rightarrow$ `artifact.TextDelta`.
- [ ] `thinking_tokens` reported in `artifact.Usage`.
- [ ] Prior turn `thinking` blocks are included in subsequent requests.
- [ ] `redacted_thinking` signatures are preserved and replayed.
- [ ] All unit and integration tests pass.
