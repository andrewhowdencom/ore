# Plan: Enhance Workshop Example with Filesystem Tools

## Objective
Wire the existing `x/tool/filesystem/` tools (`read_file`, `write_file`, `edit_file`, `list_directory`, `search_files`) into the `examples/workshop/` TUI-based coding assistant so it becomes a functional, barebones coding agent. The TUI conduit must also be enhanced to render `ToolCall` and `ToolResult` artifacts so the user can see what tools are being invoked and their outcomes.

## Context
The ore framework already contains a complete filesystem tool package (`x/tool/filesystem/`) and tool infrastructure (`x/tool/registry.go`, `x/tool/handler.go`). A prior plan (`add-filesystem-tools.md`) created these tools and a simple CLI example (`examples/filesystem/main.go`) demonstrating their use in a `cognitive.ReAct` loop.

The **workshop example** (`examples/workshop/main.go`) is a more sophisticated TUI-based coding assistant that uses:
- `session.Manager` with persistent thread storage
- `cognitive.NewTurnProcessor()` (ReAct loop)
- `x/conduit/tui/` for terminal UI
- `x/systemprompt/` and `x/guardrails/` for persona and formatting rules

**Critical gap**: The workshop's `stepFactory` creates a `loop.Step` with only transforms (system prompt, guardrails) and **no handlers or invoke options**. Consequently, the LLM is never told about available tools, and any `ToolCall` artifacts returned by the LLM are silently ignored. The workshop is currently a chatbot with no filesystem capabilities.

**TUI rendering gap**: The TUI conduit (`x/conduit/tui/model.go` and `x/conduit/tui/view.go`) only renders `artifact.Text` and `artifact.Reasoning` artifacts. `ToolCall` and `ToolResult` artifacts are completely ignored in both the model's `Update` handler and the view's `buildContent` method. If tools are wired in without fixing the TUI, the user will see empty "Assistant:" and "Tool:" lines in the conversation, with no indication of what tools were called or what they returned.

**Architecture observed**:
- `tool.Registry` maps tool names to `tool.ToolFunc` implementations and `provider.Tool` descriptors.
- `registry.Handler()` returns a `loop.Handler` that detects `artifact.ToolCall`, executes the tool, and appends a `RoleTool` turn with `artifact.ToolResult`.
- `loop.WithHandlers(registry.Handler())` registers the handler on a `loop.Step`.
- `loop.WithInvokeOptions(openai.WithTools(registry.Tools()))` pre-binds the tool metadata so the LLM receives the function definitions on every `Turn()` call.
- The `session.Manager` calls `newStep()` to create a fresh `loop.Step` for each stream/thread. The `stepFactory` is the correct injection point.
- The `cognitive.ReAct` turn processor calls `Step.Turn()` in a loop, continuing while the last turn is not `RoleAssistant`. When a `ToolCall` triggers a `RoleTool` turn, ReAct automatically calls the provider again with the tool result appended, driving the agent to observe and respond.

## Architectural Blueprint

### Selected Approach
Enhance the workshop example in three parallel tracks that converge:

1. **Workshop wiring**: Modify `examples/workshop/main.go` to create a `tool.Registry` inside the `stepFactory`, register the five filesystem tools, and pass the handler and tool invoke options into `loop.New()`.
2. **TUI enhancement**: Extend `x/conduit/tui/model.go` and `x/conduit/tui/view.go` to render `ToolCall` and `ToolResult` artifacts with clear visual labels, so the user sees what tools are invoked and what they return.
3. **Bash tool addition**: Create a new `x/tool/bash/` package with a `bash` tool for executing shell commands. A coding agent without the ability to run tests, builds, or package managers is severely limited. The tool will include basic safety (timeout, working directory restriction).

The workshop's system prompt and guardrails will be updated to explicitly encourage the agent to use its tools and to warn about destructive operations.

### Alternatives Considered
- **MCP integration**: Using the existing `x/tool/mcp/` client to discover tools from an external MCP server. Rejected because the workshop is intended as a self-contained, barebones example. MCP adds external infrastructure complexity.
- **Only filesystem tools, no bash**: Rejected because a coding agent that can read/write files but cannot run `go test`, `npm install`, or `git diff` is not practically useful.
- **In-place bash tool in workshop**: Rejected because a reusable `x/tool/bash/` package follows the ore pattern (cf. `x/tool/calculator/`, `x/tool/filesystem/`) and can be reused by other examples or applications.

## Requirements
1. Wire the five existing filesystem tools into `examples/workshop/main.go` via the `stepFactory`.
2. Enhance the TUI conduit to render `ToolCall` and `ToolResult` artifacts with human-readable labels.
3. Create a new `x/tool/bash/` package with a `bash` tool for executing shell commands, including timeout and working-directory parameters.
4. Register the `bash` tool in the workshop's tool registry.
5. Update the workshop system prompt to explicitly mention available tools and encourage their use.
6. Add a guardrail about verifying before destructive filesystem operations.
7. Ensure all changes compile and all existing tests pass.

## Task Breakdown

### Task 1: Wire Filesystem Tools into Workshop
- **Goal**: Modify `examples/workshop/main.go` to register the five filesystem tools and wire them into the `loop.Step` created by the `stepFactory`.
- **Dependencies**: None
- **Files Affected**: `examples/workshop/main.go`
- **New Files**: None
- **Interfaces**: No new interfaces; uses existing `tool.Registry`, `loop.WithHandlers`, `loop.WithInvokeOptions`, `openai.WithTools`.
- **Validation**:
  - `go build ./examples/workshop` succeeds
  - `go test ./...` passes
- **Details**:
  1. Add imports for `github.com/andrewhowdencom/ore/x/tool` and `github.com/andrewhowdencom/ore/x/tool/filesystem`.
  2. Inside the `stepFactory` closure, create a `tool.NewRegistry()`.
  3. Register all five filesystem tools using their exported descriptors and functions:
     ```go
     registry.Register(filesystem.ReadFileTool.Name, filesystem.ReadFileTool.Description, filesystem.ReadFileTool.Schema, filesystem.ReadFile)
     // Repeat for WriteFile, EditFile, ListDirectory, SearchFiles
     ```
  4. Pass the handler and invoke options to `loop.New`:
     ```go
     return loop.New(
         loop.WithTransforms(sp, gr),
         loop.WithHandlers(registry.Handler()),
         loop.WithInvokeOptions(openai.WithTools(registry.Tools())),
     ), nil
     ```
  5. Ensure `go vet ./examples/workshop` is clean.
  6. Leave the rest of the workshop (session manager, TUI conduit, signal handling) unchanged.

### Task 2: Enhance TUI to Render Tool Calls and Results
- **Goal**: Extend `x/conduit/tui/model.go` and `x/conduit/tui/view.go` to display `artifact.ToolCall` and `artifact.ToolResult` artifacts in the conversation history.
- **Dependencies**: Task 1 (logically; the TUI enhancement is necessary for usable tool UX, but can be developed in parallel since the artifact types already exist)
- **Files Affected**: `x/conduit/tui/model.go`, `x/conduit/tui/view.go`
- **New Files**: None
- **Interfaces**: No new interfaces.
- **Validation**:
  - `go test ./x/conduit/tui/...` passes
  - `go test ./...` passes
- **Details**:
  1. In `x/conduit/tui/model.go`, in the `turnMsg` handling block inside `Update`, add cases for `artifact.ToolCall` and `artifact.ToolResult`:
     - For `ToolCall`: create a `renderedBlock` with `kind: "tool_call"` and `source` set to a human-readable string like `fmt.Sprintf("Calling: %s(%s)", tc.Name, tc.Arguments)`.
     - For `ToolResult`: create a `renderedBlock` with `kind: "tool_result"` and `source` set to `tr.Content`. If `tr.IsError`, prefix the source with `"Error: "`.
  2. In `x/conduit/tui/view.go`, in `buildContent`, add handling for the new block kinds in the `RoleAssistant` and `RoleTool` switch branches:
     - `RoleAssistant` + `kind: "tool_call"`: render with label `"Assistant: "` and the call string (no Markdown rendering needed).
     - `RoleTool` + `kind: "tool_result"`: render with label `"Tool: "` and the result content.
  3. Use a subtle style (e.g., faint/italic) for tool calls to distinguish them from main assistant text. Use a distinct style (e.g., red foreground) for tool errors.
  4. Ensure the view tests in `view_test.go` are updated to cover the new rendering paths, or note that new tests should be added.

### Task 3: Create Bash Tool Package
- **Goal**: Create a reusable `x/tool/bash/` package implementing a `bash` tool for executing shell commands, following the `x/tool/filesystem/` pattern.
- **Dependencies**: None
- **Files Affected**: None (new package)
- **New Files**:
  - `x/tool/bash/go.mod`
  - `x/tool/bash/doc.go`
  - `x/tool/bash/bash.go`
  - `x/tool/bash/bash_test.go`
- **Interfaces**:
  - `Bash(ctx context.Context, args map[string]any) (any, error)` — implements `tool.ToolFunc`
  - `BashTool provider.Tool` — descriptor
- **Validation**:
  - `go test ./x/tool/bash/...` passes
  - `go test -race ./x/tool/bash/...` passes
  - `go work sync` succeeds after adding the new module
- **Details**:
  1. Add the new module to root `go.mod` (`require` + `replace`) and `go.work`.
  2. `bash.go` parameters:
     - `command` (string, required): the shell command to execute.
     - `working_directory` (string, optional): the directory to execute the command in; defaults to the current working directory.
     - `timeout_seconds` (number, optional): maximum execution time in seconds; defaults to 30.
  3. Implementation:
     - Parse `command`, `working_directory`, `timeout_seconds` from args.
     - Use `context.WithTimeout` based on `timeout_seconds`.
     - Use `exec.CommandContext` with the shell (`/bin/sh -c <command>` on Unix, `cmd /C` on Windows, or use `runtime.GOOS` to switch).
     - Set `cmd.Dir` to `working_directory` if provided.
     - Capture stdout and stderr.
     - Return a structured result: `map[string]any{"stdout": "...", "stderr": "...", "exit_code": 0}`.
     - If the command times out, return an error.
     - If the command returns a non-zero exit code, still return the output but mark it as an error result (return error from `Bash` so `tool.Handler` appends `IsError: true`).
  4. Tests:
     - `echo hello` returns stdout `"hello\n"`.
     - Invalid command returns non-zero exit code and stderr.
     - `working_directory` changes execution directory.
     - `timeout_seconds` causes timeout on `sleep 10` with timeout 1.
  5. `doc.go`: package documentation with usage example.

### Task 4: Register Bash Tool and Update System Prompt/Guardrails
- **Goal**: Register the `bash` tool in the workshop, update the system prompt to mention available tools, and add a guardrail about destructive operations.
- **Dependencies**: Task 1, Task 3
- **Files Affected**: `examples/workshop/main.go`
- **New Files**: None
- **Interfaces**: No new interfaces.
- **Validation**:
  - `go build ./examples/workshop` succeeds
  - `go test ./...` passes
- **Details**:
  1. In `examples/workshop/main.go`, add an import for `github.com/andrewhowdencom/ore/x/tool/bash`.
  2. Register the `bash` tool in the same `stepFactory` registry:
     ```go
     registry.Register(bash.BashTool.Name, bash.BashTool.Description, bash.BashTool.Schema, bash.Bash)
     ```
  3. Update the system prompt content to explicitly mention the available tools:
     ```go
     "You are a terminal-based coding assistant. You help users write, review, refactor, and debug code across any language or framework. " +
     "You have access to filesystem tools (read_file, write_file, edit_file, list_directory, search_files) and a bash tool for running shell commands. " +
     "Use these tools proactively to explore the codebase, make changes, run tests, and verify your work. " +
     "Prefer concise explanations and actionable suggestions."
     ```
  4. Update the guardrails to include a rule about destructive operations:
     ```go
     "Always format code in markdown blocks with the correct language tag.",
     "Prefer concise explanations; show code rather than prose where possible.",
     "When suggesting changes, explain the rationale briefly.",
     "Before writing or editing files, verify the target path and confirm the change is intended.",
     ```
  5. Ensure the final `examples/workshop/main.go` compiles cleanly.

## Dependency Graph
- Task 1 || Task 2 || Task 3 (parallelizable)
- Task 4 → Task 1, Task 3

## Risks & Mitigations
| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| TUI changes break existing `tui-chat` example | Medium | Medium | Run `go test ./x/conduit/tui/...` and manually verify `examples/tui-chat` still renders correctly |
| Bash tool executes dangerous commands (rm -rf /) | High | Medium | Bash tool is a barebones example; document the security risk prominently in `doc.go` and README. Future iteration can add an allowlist/blocklist. |
| LLM ignores tools or uses them incorrectly | Medium | Medium | System prompt explicitly encourages tool use; tool descriptions are clear; error messages guide the LLM to retry |
| Write/edit tools overwrite important files | High | Low | `write_file` fails if file exists; `edit_file` requires exact `old_string` match. Document that the workshop has no sandbox and operates on the host filesystem. |
| Tool results are too large for LLM context window | Medium | Low | `read_file` supports `offset` and `limit`; future iteration can add truncation to other tools |

## Validation Criteria
- [ ] `go test ./...` passes
- [ ] `go build ./examples/workshop` succeeds
- [ ] `go build ./examples/tui-chat` succeeds (TUI changes must not break other examples)
- [ ] `go build ./examples/filesystem` succeeds
- [ ] `go test ./x/tool/bash/...` passes
- [ ] `go test -race ./x/tool/bash/...` passes
- [ ] `go work sync` succeeds after adding `x/tool/bash`
- [ ] Workshop `stepFactory` creates a `tool.Registry` with 6 registered tools
- [ ] TUI model renders `ToolCall` artifacts with a "Calling:" label
- [ ] TUI model renders `ToolResult` artifacts with the result content
- [ ] TUI view displays tool calls in the `RoleAssistant` section and tool results in the `RoleTool` section
- [ ] The workshop system prompt explicitly mentions filesystem and bash tools
- [ ] The workshop guardrails include a rule about verifying before destructive operations
