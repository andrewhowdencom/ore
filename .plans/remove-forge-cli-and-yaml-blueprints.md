# Plan: Remove Forge CLI and YAML Blueprints

## Objective

Remove the `cmd/forge` build-time YAML-to-binary generator, its runtime scaffolding (`app/` package), all Forge-specific example blueprints, and the `OptionsFromMap` / `Config` / `FromConfig` bridge machinery that existed solely to support Forge YAML blueprints. Replace the Forge-only `examples/forge/workshop/` with a hand-written `examples/workshop/main.go` that composes `x/systemprompt` and `x/guardrails` transforms directly in Go code, giving those packages real consumers without YAML indirection.

## Context

Forge (`cmd/forge/`) consumed `forge.yaml` blueprints and generated compilable Go agent binaries. The `app/` package provided Cobra/Viper-based runtime scaffolding for generated binaries. The `OptionsFromMap` pattern in `x/conduit/*/config.go`, `x/systemprompt/config.go`, and `x/guardrails/config.go` bridged YAML options to Go functional options via `github.com/mitchellh/mapstructure`.

Key findings from exhaustive repository scanning:

- **Hand-written examples** (`examples/http-chat/`, `examples/tui-chat/`, `examples/calculator/`) write their own `main.go` and do **not** import `app/` or use `OptionsFromMap`.
- **`app/`** has **zero** consumers outside `cmd/forge/` (verified with `grep -r "andrewhowdencom/ore/app"` across all `.go` files).
- **`cobra` and `viper`** are only imported by `app/` and `cmd/forge/`.
- **`mapstructure`** is only imported by the `config.go` files in `x/conduit/*/`, `x/systemprompt/`, and `x/guardrails/`.
- **`x/systemprompt/`** and **`x/guardrails/`** are only used by Forge blueprints. Their core `Transform` implementations (`systemprompt.go`, `guardrails.go`) are clean, generic `loop.Transform` implementations with functional options. Only their `config.go` + `config_test.go` files are Forge-specific.
- **`examples/forge/workshop/`** was the only non-trivial Forge example. A hand-written equivalent will preserve the transform demonstration.

## Architectural Blueprint

After removal, the repository is structurally simpler:

- **Core framework** (`artifact/`, `state/`, `provider/`, `loop/`, `session/`, `thread/`, `cognitive/`, `agent/`) remains unchanged.
- **Conduit libraries** (`x/conduit/http/`, `x/conduit/tui/`, `x/conduit/slack/`, `x/conduit/telegram/`) remain, but drop their `config.go` + `config_test.go` files and their `mapstructure` dependency.
- **Transform extensions** (`x/systemprompt/`, `x/guardrails/`) remain with their core `Transform` implementations, but drop `config.go` + `config_test.go` and Forge-specific doc.go language.
- **Examples** (`examples/http-chat/`, `examples/tui-chat/`, `examples/calculator/`, `examples/single-turn-cli/`, plus new `examples/workshop/`) are all hand-written Go programs with no YAML layer.
- **Documentation** no longer references Forge, blueprints, or YAML manifests.

## Requirements

1. Delete `cmd/forge/` and all contents.
2. Delete `app/` and all contents.
3. Delete `examples/forge/` and all contents.
4. Delete `docs/reference/forge-cli.md`.
5. Delete `config.go` and `config_test.go` from all `x/conduit/*/` packages.
6. Delete `config.go` and `config_test.go` from `x/systemprompt/` and `x/guardrails/`.
7. Update `x/systemprompt/doc.go` and `x/guardrails/doc.go` to remove Forge blueprint sections.
8. Update `x/conduit/doc.go` to remove the `OptionsFromMap` bridge requirement (#6) and `mapstructure` references.
9. Create `examples/workshop/main.go` as a hand-written TUI example composing `systemprompt` and `guardrails` transforms.
10. Update `loop/doc.go` comment about `x/systemprompt` (keep reference, remove any Forge framing).
11. Update `examples/single-turn-cli/main.go` comments referencing `x/systemprompt`.
12. Update `README.md` to remove Forge references and rewrite Getting Started.
13. Update `.agents/skills/conduit/SKILL.md`, `SKELETON.md`, and `README_EXAMPLE.md` to remove Forge references.
14. Run `go mod tidy` in root and affected submodules to drop `cobra`, `viper`, and `mapstructure`.
15. Verify `go test -race ./...` passes.

## Task Breakdown

### Task 1: Remove cmd/forge CLI Tool
- **Goal**: Delete the entire `cmd/forge/` directory and all its contents.
- **Dependencies**: None.
- **Files Affected**: `cmd/forge/blueprint.go`, `cmd/forge/blueprint_test.go`, `cmd/forge/build.go`, `cmd/forge/build_test.go`, `cmd/forge/cmd_generate_test.go`, `cmd/forge/forge_test.go`, `cmd/forge/generate.go`, `cmd/forge/generate_test.go`, `cmd/forge/main.go`, `cmd/forge/main_test.go`, `cmd/forge/module.go`, `cmd/forge/module_test.go`, `cmd/forge/README.md`, `cmd/forge/templates/go.mod.tmpl`, `cmd/forge/templates/main.go.tmpl`, `cmd/forge/testdata/handler-forge.yaml`, `cmd/forge/testdata/handler-options-forge.yaml`, `cmd/forge/testdata/http-forge.yaml`, `cmd/forge/testdata/multi-forge.yaml`, `cmd/forge/testdata/tui-forge.yaml`.
- **New Files**: None.
- **Validation**: `go build ./...` passes (the removed package is no longer in the build graph).
- **Details**: Use `git rm -r cmd/forge/`. This is the build-time YAML-to-binary generator. It has no consumers outside itself.

### Task 2: Remove app/ Runtime Scaffolding Package
- **Goal**: Delete the entire `app/` directory.
- **Dependencies**: None.
- **Files Affected**: `app/app.go`, `app/app_test.go`, `app/conduits.go`, `app/config.go`, `app/config_test.go`, `app/doc.go`, `app/provider.go`.
- **New Files**: None.
- **Validation**: `go build ./...` passes. Confirm no remaining imports of `github.com/andrewhowdencom/ore/app` in the repo (`grep -r "andrewhowdencom/ore/app" --include="*.go"`).
- **Details**: The `app/` package was Cobra/Viper scaffolding for Forge-generated binaries. Hand-written examples wire their own `main.go` and never import this package.

### Task 3: Remove examples/forge/ Blueprints
- **Goal**: Delete the entire `examples/forge/` directory.
- **Dependencies**: None.
- **Files Affected**: `examples/forge/http/forge.yaml`, `examples/forge/multi/forge.yaml`, `examples/forge/tui/forge.yaml`, `examples/forge/workshop/forge.yaml`, `examples/forge/workshop/README.md`, `examples/forge/README.md`.
- **New Files**: None.
- **Validation**: `go build ./...` passes.
- **Details**: These YAML blueprints and their README only existed to demonstrate Forge. They are replaced by hand-written examples.

### Task 4: Remove docs/reference/forge-cli.md
- **Goal**: Delete the Forge CLI reference document.
- **Dependencies**: None.
- **Files Affected**: `docs/reference/forge-cli.md`.
- **New Files**: None.
- **Validation**: None specific.
- **Details**: This was dedicated documentation for `cmd/forge`. With the CLI removed, the doc is obsolete.

### Task 5: Remove OptionsFromMap Bridge from x/ Conduit Packages
- **Goal**: Delete `config.go` and `config_test.go` from all `x/conduit/*/` subdirectories.
- **Dependencies**: None.
- **Files Affected**: `x/conduit/http/config.go`, `x/conduit/http/config_test.go`, `x/conduit/tui/config.go`, `x/conduit/tui/config_test.go`, `x/conduit/slack/config.go`, `x/conduit/slack/config_test.go`, `x/conduit/telegram/config.go`, `x/conduit/telegram/config_test.go`.
- **New Files**: None.
- **Validation**: `go test ./x/conduit/http/...`, `go test ./x/conduit/tui/...`, `go test ./x/conduit/slack/...`, `go test ./x/conduit/telegram/...` all pass.
- **Details**: The `OptionsFromMap` / `FromConfig` / `Config` pattern was built exclusively for Forge YAML bridging. Hand-written examples call functional options directly (e.g., `httpc.WithAddr(":8080")`). The core conduit implementations and their other tests remain intact.

### Task 6: Clean up x/systemprompt and x/guardrails
- **Goal**: Delete Forge-specific `config.go` + `config_test.go`, update `doc.go` to remove Forge blueprint sections.
- **Dependencies**: None.
- **Files Affected**: `x/systemprompt/config.go`, `x/systemprompt/config_test.go`, `x/systemprompt/doc.go`, `x/guardrails/config.go`, `x/guardrails/config_test.go`, `x/guardrails/doc.go`.
- **New Files**: None.
- **Validation**: `go test ./x/systemprompt/...` passes, `go test ./x/guardrails/...` passes.
- **Details**: In each `doc.go`, remove the `# Forge Blueprint Usage` section (YAML example) and promote the `# Hand-Compiled Usage` section as the primary documentation. The core `systemprompt.go`/`guardrails.go` and their transform tests remain.

### Task 7: Update x/conduit/doc.go Standard Contract
- **Goal**: Remove requirement #6 (`OptionsFromMap bridge`) and `mapstructure` references.
- **Dependencies**: Task 5.
- **Files Affected**: `x/conduit/doc.go`.
- **New Files**: None.
- **Validation**: None specific.
- **Details**: Delete the entire bullet #6 about `OptionsFromMap`. Remove the sentence "Every conduit package MUST export OptionsFromMap so that the forge code generator and the runtime app package can translate YAML/JSON configuration maps..." and the `mapstructure` reference. The remaining 5 requirements (constructor, Descriptor, sink registration, blocking Start, graceful shutdown) stay.

### Task 8: Create examples/workshop/main.go
- **Goal**: Create a hand-written TUI example that composes `x/systemprompt` and `x/guardrails` transforms, replacing the Forge-only workshop blueprint.
- **Dependencies**: Task 6 (systemprompt/guardrails must be clean first).
- **Files Affected**: None (new file).
- **New Files**: `examples/workshop/main.go`.
- **Validation**: `go build ./examples/workshop` passes.
- **Details**: Pattern after `examples/tui-chat/main.go`. Add `loop.WithTransforms(...)` to the `Step` factory, importing `x/systemprompt` and `x/guardrails` directly:
  ```go
  stepFactory := func() (*loop.Step, error) {
      sp, _ := systemprompt.New(systemprompt.WithContent("You are a terminal-based coding assistant..."))
      gr, _ := guardrails.New(guardrails.WithRules(
          "Always format code in markdown blocks with the correct language tag.",
          "Prefer concise explanations; show code rather than prose where possible.",
          "When suggesting changes, explain the rationale briefly.",
      ))
      return loop.New(loop.WithTransforms(sp, gr)), nil
  }
  ```
  Include a standard package comment with usage instructions and `ORE_*` environment variable documentation.

### Task 9: Update Comment References in loop/doc.go and single-turn-cli
- **Goal**: Remove or update comments that reference Forge or show `x/systemprompt` in a Forge context.
- **Dependencies**: Task 6, Task 8.
- **Files Affected**: `loop/doc.go`, `examples/single-turn-cli/main.go`.
- **New Files**: None.
- **Validation**: `go build ./examples/single-turn-cli` passes.
- **Details**: In `loop/doc.go`, the reference to `x/systemprompt` as a reusable transform is valid — keep it, but ensure no Forge framing remains. In `examples/single-turn-cli/main.go`, update the commented-out `x/systemprompt` example (around line 106) to show hand-compiled usage rather than a blueprint-style snippet.

### Task 10: Update README.md
- **Goal**: Remove all Forge references and rewrite Getting Started to point to hand-written examples.
- **Dependencies**: Task 1, Task 2, Task 3.
- **Files Affected**: `README.md`.
- **New Files**: None.
- **Validation**: `grep -i "forge\|blueprint\|forge.yaml" README.md` returns zero matches.
- **Details**: Remove the `cmd/forge` row from the Packages table. Rewrite the "Getting Started" section (currently pointing to Forge) to point to `examples/http-chat/`, `examples/tui-chat/`, and `examples/workshop/`. Remove any references to `forge.yaml` or blueprint composition.

### Task 11: Update .agents/skills/conduit/ Documentation
- **Goal**: Remove all Forge references from the conduit skill and rewrite for hand-composition.
- **Dependencies**: None.
- **Files Affected**: `.agents/skills/conduit/SKILL.md`, `.agents/skills/conduit/SKELETON.md`, `.agents/skills/conduit/README_EXAMPLE.md`.
- **New Files**: None.
- **Validation**: `grep -i "forge\|blueprint\|forge.yaml\|cmd/forge" .agents/skills/conduit/*` returns zero matches.
- **Details**: In `SKILL.md`: remove step 1's "declarable in a `forge.yaml`" language; remove step 3's "`cmd/forge` calls `alias.New(mgr)`" note; remove the "Is declarable in a `forge.yaml`" success criterion; remove the "Forge calls `alias.New(mgr)` with no arguments" gotcha. In `SKELETON.md`: update the closing note about "forge and multi-conduit patterns." In `README_EXAMPLE.md`: remove or rewrite the "Forge Blueprint" section. Read the full `README_EXAMPLE.md` (it was truncated at line 50 during discovery) to catch any hidden Forge references.

### Task 12: Clean up go.mod Dependencies
- **Goal**: Drop `cobra`, `viper`, and `mapstructure` from go.mod files where they are no longer needed.
- **Dependencies**: Task 1, Task 2, Task 5, Task 6.
- **Files Affected**: `go.mod`, `x/conduit/http/go.mod`, `x/conduit/tui/go.mod`, `x/conduit/slack/go.mod`, `x/conduit/telegram/go.mod`, `go.work.sum`.
- **New Files**: None.
- **Validation**: `go build ./...` passes after tidy. `go mod graph | grep -E 'cobra|viper|mapstructure'` returns nothing at the root.
- **Details**: Run `go mod tidy` at the repository root — `cobra` and `viper` should drop from the root `go.mod` because `app/` and `cmd/forge/` are gone. `mapstructure` should also drop from the root because `x/systemprompt/config.go` and `x/guardrails/config.go` are gone. Run `go mod tidy` in each `x/conduit/*/` submodule — `mapstructure` should drop because `config.go` is gone. Run `go work sync` if needed to refresh `go.work.sum`.

### Task 13: Final Verification
- **Goal**: Run full test suite with race detector to confirm nothing is broken.
- **Dependencies**: Task 8, Task 9, Task 10, Task 11, Task 12.
- **Files Affected**: All.
- **New Files**: None.
- **Validation**: `go test -race ./...` passes with zero failures.
- **Details**: This is the final gate. If any test fails, trace it to an undeleted import, stale go.mod entry, or missed file reference.

## Dependency Graph

```
Task 1  ||  Task 2  ||  Task 3  ||  Task 4  ||  Task 5  ||  Task 6  ||  Task 7  ||  Task 9  ||  Task 11
                                                                                                         \
Task 8  →  (depends on Task 6)                                                                            \
                                                                                                           \
Task 10  →  (depends on Task 1, 2, 3)                                                                      \
                                                                                                             \
Task 12  →  (depends on Task 1, 2, 5, 6)                                                                      \
                                                                                                               \
Task 13  →  (depends on Task 8, 9, 10, 11, 12)
```

In prose:
- Tasks 1, 2, 3, 4, 5, 6, 7, 9, and 11 are all deletions or independent updates and can proceed in parallel.
- Task 8 (create workshop example) depends on Task 6 (systemprompt/guardrails must be clean first).
- Task 10 (README update) depends on Tasks 1, 2, 3 (knowing what's been deleted).
- Task 12 (go mod tidy) depends on Tasks 1, 2, 5, 6 (all deletion tasks that affect imports).
- Task 13 (final verify) gates on Tasks 8, 9, 10, 11, 12.

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Undiscovered import of `app/` or `cmd/forge/` outside those directories | High | Low | Pre-verified with `grep -r "andrewhowdencom/ore/app" --include="*.go"` and `grep -ri "forge" --include="*.go"` across the repo. Re-run after all deletions. |
| `go mod tidy` leaves indirect dependencies lingering | Low | Medium | Inspect `go.mod` manually after tidy. Run `go mod graph \| grep -E 'cobra\|viper\|mapstructure'` to confirm nothing references them. |
| `examples/workshop/main.go` fails to compile due to transform API mismatch | Medium | Low | Model it exactly after `examples/tui-chat/main.go` with `loop.WithTransforms(...)` added. Build immediately after writing. |
| `.agents/skills/conduit/README_EXAMPLE.md` contains hidden Forge references in sections not yet read | Low | Medium | Read the full file (it was truncated at line 50 during discovery) and scan for "forge", "blueprint", "yaml" during implementation. |
| `.plans/` files referencing Forge become historical dead weight | Low | Low | Leave them as-is; they are design artifacts and do not affect the build. Optionally delete later as cleanup. |

## Validation Criteria

- [ ] `cmd/forge/` directory no longer exists.
- [ ] `app/` directory no longer exists.
- [ ] `examples/forge/` directory no longer exists.
- [ ] `docs/reference/forge-cli.md` no longer exists.
- [ ] No `config.go` or `config_test.go` remains in any `x/conduit/*/` package.
- [ ] No `config.go` or `config_test.go` remains in `x/systemprompt/` or `x/guardrails/`.
- [ ] `examples/workshop/main.go` exists and compiles (`go build ./examples/workshop` passes).
- [ ] `grep -ri "forge" --include="*.go" --include="*.md"` across the repo (excluding `.plans/` and `.worktrees/`) returns zero matches.
- [ ] `go build ./...` passes.
- [ ] `go test -race ./...` passes.
- [ ] Root `go.mod` does not list `cobra`, `viper`, or `mapstructure` as direct dependencies.
- [ ] Submodule `go.mod` files (`x/conduit/http`, `x/conduit/tui`, `x/conduit/slack`, `x/conduit/telegram`) do not list `mapstructure` as a direct dependency.
