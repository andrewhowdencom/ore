# Plan: Design Surface Capability Contract

## Objective
Introduce a semantic metadata-based capability model for ore surfaces. Replace the monolithic `Surface` interface's `SetStatus` method with a `Capable` contract that lets surfaces declare what they can do via string-based capability tags. Add a `Descriptor` pattern for static capability declaration and a doc generator that produces a compatibility matrix. This enables planning multi-surface features in terms of abstract capabilities before mapping them to concrete implementations.

## Context
The ore framework's `surface.Surface` interface (`surface/surface.go`) currently couples two concerns: ingress (`Events() <-chan Event`) and a medium-specific egress method (`SetStatus(ctx, string) error`). The `SetStatus` signature is TUI-centric — Telegram, voice, and web surfaces would need different signatures. The framework itself (`loop.Step`, `cognitive.ReAct`) never calls `SetStatus`; only `examples/tui-chat/main.go` does, and it operates on a concrete `*tui.TUI` type.

The `Surface` interface is barely used as an interface value in the framework. Only `surface/tui/tui.go` claims to satisfy it. This makes the refactor low-risk.

After ideation, we converged on a metadata-only capability model:
- Capabilities are string tags (e.g., `"show-status"`, `"render-delta"`), not Go interfaces prescribing method signatures.
- Surfaces declare capabilities via a package-level `Descriptor` variable.
- Surfaces implement `Capable` (`Capabilities() []Capability`, `Can(Capability) bool`).
- `Surface` is refactored to embed `Capable` + `Events() <-chan Event`, removing `SetStatus`.
- A `cmd/docgen` program reads `Descriptor` values and emits a Markdown compatibility matrix.

## Architectural Blueprint

### Capability Model
Capabilities are semantic metadata — strings that describe what a surface *can* do. The framework (and application builders) queries these tags to adapt behavior or plan features. The actual execution mechanism is medium-specific and lives outside the capability contract.

```
surface/
  surface.go          — Capability type, Capable interface, Descriptor, Surface refactor
  surface_test.go     — tests for Capable, Descriptor, constants
  tui/
    tui.go            — TUI implements Capable, exports Descriptor
    tui_test.go       — compile-time checks + Can() tests

cmd/
  docgen/
    main.go           — reads Descriptor from surface packages, writes Markdown matrix

docs/
  surface-capabilities.md  — generated compatibility matrix
```

### Surface Interface Refactor
The current `Surface` interface:
```go
type Surface interface {
    Events() <-chan Event
    SetStatus(ctx context.Context, status string) error
}
```

Becomes:
```go
type Surface interface {
    Capable
    Events() <-chan Event
}
```

`SetStatus` is removed from the framework contract. It remains on `*tui.TUI` as a concrete method, since the TUI example (`examples/tui-chat/main.go`) uses the concrete type directly.

### Doc Generator
`cmd/docgen/main.go` imports each surface package, reads its `Descriptor`, and produces a Markdown table:

```markdown
| Capability | TUI | Telegram | Web | Voice |
|------------|-----|----------|-----|-------|
| event-source | ✅ | ❌ | ❌ | ❌ |
| show-status | ✅ | ❌ | ❌ | ❌ |
| render-delta | ✅ | ❌ | ❌ | ❌ |
| ... | ... | ... | ... | ... |
```

The generator requires manual registration of surface packages (explicit import list). Future surfaces are added by updating the import list in `cmd/docgen/main.go`.

## Requirements
1. Define `Capability` as a string type with well-known constants for common surface abilities.
2. Define `Descriptor` struct with `Name`, `Description`, and `Capabilities` fields.
3. Define `Capable` interface with `Capabilities() []Capability` and `Can(Capability) bool`.
4. Refactor `Surface` to embed `Capable` and `Events()`, removing `SetStatus`.
5. Update `surface/tui` to implement `Capable` and export a `Descriptor`.
6. Create `cmd/docgen/main.go` that reads `Descriptor` values and generates a Markdown matrix.
7. Generate and check in the initial `docs/surface-capabilities.md`.
8. All existing tests and examples must continue to compile and pass.

## Task Breakdown

### Task 1: Add Capability Model and Refactor Surface Interface
- **Goal**: Add `Capability`, `Descriptor`, `Capable` types; refactor `Surface` to embed `Capable` and remove `SetStatus`.
- **Dependencies**: None.
- **Files Affected**:
  - `surface/surface.go`
  - `surface/surface_test.go`
- **New Files**: None.
- **Interfaces**:
  - `type Capability string`
  - `type Descriptor struct { Name string; Description string; Capabilities []Capability }`
  - `type Capable interface { Capabilities() []Capability; Can(cap Capability) bool }`
  - `type Surface interface { Capable; Events() <-chan Event }`
- **Validation**:
  - `go test -race ./surface/...` passes.
  - `go build ./...` passes.
  - `go vet ./...` clean.
- **Details**:
  - Define `Capability` as a string type.
  - Add well-known constants: `CapEventSource`, `CapShowStatus`, `CapRenderDelta`, `CapRenderTurn`, `CapRenderMarkdown`, `CapRenderImage`, `CapRenderAudio`, `CapAcceptText`, `CapAcceptImage`, `CapAcceptVoice`, `CapAcceptFile`, `CapShowTypingIndicator`, `CapRenderInlineButtons`, `CapRequestUserConfirm`.
  - Define `Descriptor` with `Name`, `Description`, `Capabilities []Capability`.
  - Define `Capable` interface.
  - Refactor `Surface` to embed `Capable` and keep only `Events() <-chan Event`. Remove `SetStatus`.
  - Add a helper `contains(caps []Capability, cap Capability) bool` (simple loop; avoid `slices` package to be safe against the unusual Go version in `go.mod`).
  - Update doc comments on `Surface` to explain the capability model.
  - Add tests: verify `Capable` interface shape, test `Can()` with known/unknown capabilities, verify well-known constants are non-empty.

### Task 2: Update TUI Surface to Declare Capabilities
- **Goal**: Make `*tui.TUI` implement `Capable` and export a `Descriptor`.
- **Dependencies**: Task 1.
- **Files Affected**:
  - `surface/tui/tui.go`
- **New Files**: None.
- **Interfaces**:
  - `func (t *TUI) Capabilities() []surface.Capability`
  - `func (t *TUI) Can(cap surface.Capability) bool`
- **Validation**:
  - `go test -race ./surface/tui/...` passes.
  - `go build ./...` passes.
  - `examples/tui-chat/main.go` compiles (`go build ./examples/tui-chat/...`).
- **Details**:
  - Add package-level `Descriptor` variable:
    ```go
    var Descriptor = surface.Descriptor{
        Name:        "TUI",
        Description: "Terminal user interface via Bubble Tea",
        Capabilities: []surface.Capability{
            surface.CapEventSource,
            surface.CapShowStatus,
            surface.CapRenderDelta,
            surface.CapRenderTurn,
            surface.CapRenderMarkdown,
        },
    }
    ```
  - Implement `Capabilities() []surface.Capability` returning `Descriptor.Capabilities`.
  - Implement `Can(cap surface.Capability) bool` using a helper (or `contains`).
  - Keep `SetStatus` and `Run` as concrete methods on `*TUI` — they are not part of the `Surface` interface.
  - Update the package doc comment or any comment claiming the TUI "satisfies surface.Surface" to be accurate (it now satisfies `Surface` via `Capable` + `Events`).

### Task 3: Add TUI Capability Tests
- **Goal**: Verify `*TUI` satisfies `Surface` and `Capable`; test `Can()` behavior.
- **Dependencies**: Task 2.
- **Files Affected**:
  - `surface/tui/tui_test.go`
- **New Files**: None.
- **Interfaces**: Compile-time checks for `Surface` and `Capable`.
- **Validation**:
  - `go test -race ./surface/tui/...` passes.
- **Details**:
  - Add compile-time interface checks:
    ```go
    var _ surface.Surface = (*TUI)(nil)
    var _ surface.Capable = (*TUI)(nil)
    ```
  - Add table-driven tests for `Capabilities()`:
    - Verify the returned slice contains expected capabilities.
    - Verify the returned slice does not contain unexpected capabilities.
  - Add table-driven tests for `Can()`:
    - Returns `true` for declared capabilities.
    - Returns `false` for undeclared capabilities.
    - Returns `false` for empty/unknown capability.

### Task 4: Create Capability Matrix Doc Generator
- **Goal**: Create `cmd/docgen/main.go` that reads `Descriptor` from surface packages and writes a Markdown compatibility matrix.
- **Dependencies**: Task 2 (TUI must have `Descriptor` available).
- **Files Affected**: None.
- **New Files**:
  - `cmd/docgen/main.go`
- **Interfaces**:
  - `func main()` with `-out` flag (default `docs/surface-capabilities.md`).
- **Validation**:
  - `go build ./cmd/docgen/...` succeeds.
  - `go run ./cmd/docgen -out /tmp/test.md` produces valid markdown.
- **Details**:
  - Import `github.com/andrewhowdencom/ore/surface/tui` to read `tui.Descriptor`.
  - Maintain an explicit slice of descriptors to document:
    ```go
    var surfaces = []surface.Descriptor{
        tui.Descriptor,
    }
    ```
  - Collect all unique capabilities across all descriptors.
  - Generate a Markdown table with:
    - Rows: capabilities (sorted alphabetically or by category).
    - Columns: surface names from `Descriptor.Name`.
    - Cells: `✅` if `Can()` or descriptor contains it, `❌` otherwise.
  - Add a header comment explaining the file is generated.
  - Create parent directories for the output path if they don't exist (`os.MkdirAll`).
  - Use `flag` package for `-out` and optionally `-title`.
  - Follow project error handling: `fmt.Errorf("...: %w", err)`.
  - Use `log/slog` for lifecycle logging (startup, success, errors).

### Task 5: Generate and Commit Initial Capability Matrix
- **Goal**: Run the doc generator and check in the initial `docs/surface-capabilities.md`.
- **Dependencies**: Task 4.
- **Files Affected**: None.
- **New Files**:
  - `docs/surface-capabilities.md`
- **Interfaces**: None.
- **Validation**:
  - `docs/surface-capabilities.md` exists and renders correctly as Markdown.
  - File contains the TUI column with expected capabilities marked ✅.
  - File contains a header comment indicating it is generated.
- **Details**:
  - Run `go run ./cmd/docgen -out docs/surface-capabilities.md`.
  - Visually inspect the output for correctness.
  - The file should be checked into version control as part of the implementation.
  - Add a note to `README.md` or `AGENTS.md` (optional — if the builder sees a natural place) explaining that `docs/surface-capabilities.md` is auto-generated.

## Dependency Graph
- Task 1 → Task 2
- Task 2 → Task 3
- Task 2 → Task 4
- Task 4 → Task 5
- Task 3 || Task 4 (parallel after Task 2)

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Removing `SetStatus` from `Surface` breaks external consumers | Medium | Low | The framework is under active development; breaking changes are expected. Document in commit message. The only known consumer is `examples/tui-chat/main.go`, which uses the concrete `*tui.TUI` type. |
| `go.mod` declares `go 1.26.2` which does not exist as a stable release | Low | High | Use only basic Go constructs (loops, maps, slices) that are stable across all Go versions. Avoid `slices` package from stdlib. Use a simple `contains` helper. |
| Doc generator requires manual update when new surface packages are added | Low | High | Document in `cmd/docgen/main.go` that the `surfaces` slice must be updated. The generated file header also indicates the source. |
| Future surfaces need different `Can()` implementations (e.g., dynamic capabilities) | Medium | Low | The current design uses static `Descriptor` values. If dynamic capabilities are needed later, `Capabilities()` can be overridden on the concrete type without changing the interface. |
| Capability constant namespace collisions as the ecosystem grows | Low | Medium | Use a `surface.` prefix (e.g., `surface.CapRenderDelta`) and a short, consistent naming convention. Document the naming convention. |

## Validation Criteria
- [ ] `go test -race ./...` passes after every task.
- [ ] `go build ./...` passes after every task.
- [ ] `go vet ./...` is clean.
- [ ] `examples/tui-chat/main.go` compiles (`go build ./examples/tui-chat/...`).
- [ ] `*tui.TUI` satisfies `surface.Surface` and `surface.Capable` at compile time.
- [ ] `docs/surface-capabilities.md` is generated and contains the TUI capability matrix.
- [ ] The `Surface` interface no longer contains `SetStatus`.
- [ ] Well-known capability constants are defined and non-empty.
