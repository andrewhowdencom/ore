# Plan: Structured Handoff Summary Prompt

## Objective

Redesign the `SummarizeStrategy` default prompt to produce a structured, deterministic, two-pass handoff summary with markdown-formatted sections. Add a `Prompt` field to `SummarizeStrategy` so applications can override the default prompt while preserving a sensible, domain-agnostic structured default that works across chat, coding, and research agents.

## Context

### Current Implementation

`x/compaction/summarize.go` defines `SummarizeStrategy` with a hardcoded generic prompt:

```go
buf.Append(state.RoleUser, artifact.Text{
    Content: "Summarize the above conversation concisely, preserving all key facts, decisions, and context.",
})
```

This produces inconsistent, non-deterministic summaries. Different runs omit different categories of information, and the model has no structural guidance on what to preserve.

### Repository Findings

- `SummarizeStrategy` is **only referenced within `x/compaction`** — no external consumers in the repository. `grep -r "SummarizeStrategy" --include="*.go" . | grep -v "\.worktrees/" | grep -v "x/compaction/"` returns no matches.
- The package uses **plain structs** for strategies (`KeepLastN`, `TurnCountTrigger`, `TokenUsageTrigger`) — there are no constructors with functional options. Adding a field is consistent with existing patterns.
- `SummarizeStrategy` already has `Provider` and `MaxTokens` fields. Adding a `Prompt` field is a natural extension.
- `state.Buffer` is used to load turns and append the user prompt turn before calling `Provider.Invoke()`.
- The `mockProvider` test double captures `receivedTurns` so the prompt content can be asserted in tests.

### Project Conventions

- `AGENTS.md`: "At this stage of the project, prefer aggressive refactoring — rename packages, move files, delete indirection, and break internal APIs when doing so produces cleaner module boundaries."
- The compactor is a state reducer, not a lens. Compaction is destructive via `LoadTurns()`.
- `state.Buffer` is not safe for concurrent use. `Compact` must be called from the same goroutine as `step.Turn()`.

## Architectural Blueprint

### Selected Approach: Prompt Field with Default Constant

Add a `Prompt string` field to `SummarizeStrategy`. When `Prompt` is empty, the `Compact` method uses a package-level unexported `defaultPrompt` constant. When `Prompt` is non-empty, it uses the user-provided prompt. This is consistent with the existing plain-struct pattern in `x/compaction` and requires zero API breakage for existing code.

**Why not a constructor with functional options?** `KeepLastN`, `TurnCountTrigger`, and `TokenUsageTrigger` are all plain structs with no constructors. Adding a constructor for `SummarizeStrategy` would introduce an inconsistent pattern and unnecessary indirection for a struct with three fields.

**Why not a `Compactor`-level option?** A `WithSummarizePrompt` option on `Compactor` would require type-asserting through the `Strategy` interface slice to find `SummarizeStrategy`, breaking the clean abstraction between compactor and strategies.

**Prompt design:** The default prompt is a single multi-line string that instructs the model to first internally analyze the conversation, then output ONLY a structured markdown summary with five sections. The analysis phase is internal reasoning (not emitted), and the output is strictly the markdown sections. This avoids the need for the implementation to strip intermediate analysis blocks.

The five sections are domain-agnostic and applicable to any agentic workflow:

1. **Primary Goal** — what the user is trying to accomplish
2. **Key Decisions & Constraints** — rules, preferences, architectural choices
3. **Completed Work** — what has already been successfully done
4. **Current State / Work in Progress** — what is actively being worked on
5. **Pending Tasks & Next Steps** — what still needs to be done

This aligns with the research-backed "task-oriented handoff document" approach described in the context document, while remaining generic enough for any ore-based application.

## Requirements

1. `SummarizeStrategy` must gain a `Prompt string` field for custom prompt override.
2. When `Prompt` is empty, `Compact` must use a default structured handoff prompt with markdown sections.
3. When `Prompt` is non-empty, `Compact` must use the provided prompt verbatim.
4. The default prompt must instruct the model to internally analyze the conversation before outputting only the structured markdown summary.
5. The default prompt must use exactly five markdown sections: Primary Goal, Key Decisions & Constraints, Completed Work, Current State / Work in Progress, Pending Tasks & Next Steps.
6. The default prompt must be stored as an unexported package-level constant.
7. All existing `x/compaction` tests must continue to pass without modification (the mock provider does not depend on prompt content).
8. New tests must verify both default prompt usage and custom prompt override behavior.
9. `x/compaction/doc.go` must document the `Prompt` field and the default structured prompt behavior.

## Task Breakdown

### Task 1: Add Default Prompt and Prompt Field
- **Goal**: Add the `defaultPrompt` constant and `Prompt` field to `SummarizeStrategy`, and wire the field into `Compact`.
- **Dependencies**: None.
- **Files Affected**: `x/compaction/summarize.go`
- **New Files**: None.
- **Interfaces**:
  - `SummarizeStrategy` struct: add `Prompt string` field.
  - `Compact` method: change prompt construction from hardcoded string to `prompt := s.Prompt; if prompt == "" { prompt = defaultPrompt }`.
- **Validation**:
  - `go build ./x/compaction/...` compiles.
  - `go test ./x/compaction/...` passes (existing tests are unaffected).
- **Details**:
  - Add an unexported `defaultPrompt` constant (raw string literal, backticks) containing the structured handoff prompt. The exact text:

    ```
    You are creating a context handoff summary for another agent that will resume this conversation.

    First, analyze the conversation history to identify:
    - The primary goal or task the user is trying to accomplish
    - Key decisions, constraints, preferences, and rules established
    - Completed work and successful outcomes
    - Current state and work in progress
    - Pending tasks and next steps required

    Then, output ONLY a structured summary using the following markdown sections. Do not include any analysis, reasoning, introductory text, or commentary outside these sections. The summary must be concise but complete — another agent will resume work from this summary.

    ## Primary Goal
    [What the user is trying to accomplish]

    ## Key Decisions & Constraints
    [Important rules, preferences, architectural choices, or constraints established]

    ## Completed Work
    [What has already been successfully done]

    ## Current State / Work in Progress
    [What is actively being worked on, including any in-progress decisions or partial results]

    ## Pending Tasks & Next Steps
    [What still needs to be done to complete the goal]
    ```

  - Update `SummarizeStrategy` struct doc comment to document the `Prompt` field: "Prompt is an optional custom summarization prompt. When empty, a default structured handoff prompt is used."
  - In `Compact`, replace the inline `artifact.Text{Content: "Summarize..."}` with:
    ```go
    prompt := s.Prompt
    if prompt == "" {
        prompt = defaultPrompt
    }
    buf.Append(state.RoleUser, artifact.Text{Content: prompt})
    ```
  - This change must not alter the control flow, error handling, or the `RoleUser` / `RoleSystem` role assignments.

### Task 2: Add Tests for Prompt Override
- **Goal**: Add tests that verify the default prompt is used when `Prompt` is empty and a custom prompt is used when `Prompt` is set.
- **Dependencies**: Task 1.
- **Files Affected**: `x/compaction/summarize_test.go`
- **New Files**: None.
- **Interfaces**: None.
- **Validation**:
  - `go test -race ./x/compaction/...` passes.
- **Details**:
  - **Test case: `TestSummarizeStrategy_UsesDefaultPrompt`** — Create a `SummarizeStrategy` with `Prompt: ""` and `MaxTokens: 1`. Provide a mock provider that captures `receivedTurns`. Assert that the last user turn's text content equals `defaultPrompt`. Use the existing `mockProvider` infrastructure; inspect `receivedTurns[len(receivedTurns)-1].Artifacts[0]` as `artifact.Text`.
  - **Test case: `TestSummarizeStrategy_UsesCustomPrompt`** — Create a `SummarizeStrategy` with `Prompt: "Custom override prompt"` and `MaxTokens: 1`. Assert that the last user turn's text content equals the custom prompt string.
  - Both tests should use minimal turn data (e.g., two turns of 1 token each) to trigger summarization. Configure the mock provider to return a single `artifact.Text{Content: "Summary."}` so the test completes without errors.
  - Use `assert.Contains` or `assert.Equal` for the prompt content check. The tests must be table-driven if they share setup logic.

### Task 3: Update Package Documentation
- **Goal**: Update `x/compaction/doc.go` to document the `Prompt` field and the default structured prompt behavior.
- **Dependencies**: Task 1.
- **Files Affected**: `x/compaction/doc.go`
- **New Files**: None.
- **Interfaces**: None.
- **Validation**:
  - `go test ./x/compaction/...` passes (doc changes do not break code).
- **Details**:
  - In the `# Built-in Strategies` section, update the `SummarizeStrategy` paragraph to:
    ```
    SummarizeStrategy is a token-aware strategy that calls an LLM provider to
    summarize conversation history. It walks backwards from the last turn,
    preserving the suffix that fits within MaxTokens, and replaces the prefix
    with a single synthetic system summary turn. If the entire history fits,
    it returns a defensive copy without calling the provider. Token estimation
    is a rough heuristic (len(text)/4) with no external dependencies.
    The summary turn uses RoleSystem because it is injected context about
    prior conversation, not a real assistant response.

    SummarizeStrategy uses a default structured handoff prompt that produces
    markdown output with five sections: Primary Goal, Key Decisions &
    Constraints, Completed Work, Current State / Work in Progress, and Pending
    Tasks & Next Steps. Applications can override the prompt via the Prompt
    field.
    ```
  - The application wiring example does not need to change since `Prompt` is optional.

## Dependency Graph

- Task 1 → Task 2 (Task 2 depends on the new Prompt field and defaultPrompt constant)
- Task 1 → Task 3 (Task 3 depends on the new API)
- Task 2 || Task 3 (parallelizable after Task 1)

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Default prompt is too long, increasing summarization token cost | Low | Medium | The prompt is ~1.5KB of text; this is negligible compared to the conversation history being summarized. Better summary quality justifies the cost. |
| Default prompt does not work well with all providers (e.g., local models with weak instruction following) | Medium | Low | The prompt uses simple, explicit instructions and standard markdown. The `Prompt` field allows applications to substitute a provider-specific prompt. |
| Existing external consumers (outside this repository) reference the hardcoded prompt string | Low | High | grep confirmed no references in this repo. The hardcoded string was never exported. External consumers would have had to vendor or copy the code; the `Prompt` field is additive and does not break their usage. |
| Multi-line raw string literal in Go source is tricky to format correctly | Low | Low | Use backticks. Ensure no leading/trailing blank lines that would affect the prompt content. |

## Validation Criteria

- [ ] `x/compaction/summarize.go` contains an unexported `defaultPrompt` constant with the structured handoff prompt text.
- [ ] `SummarizeStrategy` has a `Prompt string` field with documented semantics.
- [ ] `Compact` uses `s.Prompt` when non-empty, otherwise `defaultPrompt`.
- [ ] `go test ./x/compaction/...` passes with all existing tests.
- [ ] `go test -race ./x/compaction/...` passes including new prompt override tests.
- [ ] `go vet ./...` is clean.
- [ ] `x/compaction/doc.go` documents the `Prompt` field and the default structured prompt behavior.
- [ ] No files outside `x/compaction` reference `SummarizeStrategy` or its prompt (verified by grep).
