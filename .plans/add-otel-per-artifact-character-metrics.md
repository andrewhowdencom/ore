# Plan: Add OTel Per-Artifact Character Metrics and Version Resource Attribute

## Objective

Add OpenTelemetry metrics that expose cumulative character counts per artifact kind (e.g. `text`, `tool_call`, `tool_result`, `reasoning`, `image`) and per turn role (`user`, `assistant`, `tool`, `system`), plus demonstrate wiring an application-level version resource attribute (`service.version`) into the OTel resource. This gives operators visibility into where prompt/completion characters are spent across sessions over time.

## Context

The framework currently provides token-level visibility via `artifact.Usage` and the `x/usage` handler (which aggregates into `PropertiesEvent` for the TUI status bar). However, there is no per-artifact breakdown of character spend, making it impossible to attribute cost growth to user text, tool calls, or large tool results.

Key architectural patterns observed:

- `loop.Step` emits `TurnCompleteEvent` via `EventBus.Emit`, which first runs synchronous `OnEmit` callbacks, then forwards to the async `FanOut`.
- `loop.OnEmit` callbacks receive every `OutputEvent` and are the canonical hook for lossless, ordered side-effects (logging, metrics, state persistence).
- `loop.Handler` processes individual `artifact.Artifact` values, not events. It is the wrong abstraction for observing `TurnCompleteEvent`.
- `x/usage` is a `loop.Handler` that watches `artifact.Usage` artifacts and emits `loop.PropertiesEvent`. The TUI renders these as `↑ X · ↓ Y · Σ Z` in the status bar.
- OTel tracing is already wired via `WithTracer(trace.Tracer)` functional options on `Step`, `Provider`, and `TUI`.
- No OTel metrics are currently used in the framework.
- `go.opentelemetry.io/otel/metric v1.44.0` is already an indirect dependency in `examples/go.mod`.
- The examples live in a separate `examples/go.mod` module, so adding OTel SDK dependencies there does not bloat the root module.
- The `go.work` file at root defines the workspace; `x/telemetry` must be added to it.

Files read during discovery:
- `loop/loop.go` — `OutputEvent`, `TurnCompleteEvent`, `OnEmit`, `WithOnEmit`
- `loop/handler.go` — `Handler` interface (processes artifacts, not events)
- `loop/eventbus.go` — `EventBus.Emit` runs `OnEmit` callbacks synchronously before `FanOut`
- `artifact/artifact.go` — artifact types, `LLMString()` on `ToolCall` and `ToolResult`
- `state/state.go` — `Role` enum (`user`, `assistant`, `tool`, `system`) and `Turn` struct
- `x/usage/handler.go` — existing usage aggregation handler
- `session/manager.go` — `Manager` creates `Step` with `defaultOnEmit` + factory options
- `examples/tui-chat/main.go` — uses `noop.NewTracerProvider()` and `usage.New()`
- `examples/http-chat/main.go` — uses `noop.NewTracerProvider()` and `usage.New()`
- `examples/go.mod` — examples module with indirect `go.opentelemetry.io/otel/metric`
- `go.work` — workspace definition for all modules

## Architectural Blueprint

A new extension module `x/telemetry` provides an `OnEmit` callback (not a `Handler`) that watches `TurnCompleteEvent`, iterates over `Turn.Artifacts`, computes character counts per artifact, and records OTel counters with `artifact.kind` and `role` attributes. The version resource attribute is an application-level concern demonstrated in the example applications, not a framework concern.

Selected path: **Create `x/telemetry` as a new module** (rather than extending `x/usage`) because:
- `x/usage` is a `loop.Handler` (artifact-oriented); telemetry needs `OnEmit` (event-oriented). Mixing the two abstractions in one package would confuse the API.
- `x/usage` emits `PropertiesEvent` which the TUI depends on for its status bar. Deprecating it is a future concern; for now we keep `x/usage` intact and add telemetry alongside it.
- A separate package gives a clean boundary: `x/telemetry` depends only on `go.opentelemetry.io/otel/metric`, while `x/usage` depends only on the core `loop` and `artifact` packages.

The `OnEmit` callback is chosen over a `Handler` because `Handler.Handle` receives individual `artifact.Artifact` values, not `TurnCompleteEvent`. The `OnEmit` tier is the correct hook for observing turn boundaries and recording metrics with both artifact-level and role-level attribution.

## Requirements

1. New OTel metric counters `ore.llm.characters.sent` and `ore.llm.characters.received` with attributes `artifact.kind` and `role`.
2. Counter `sent` increments for `user`, `system`, and `tool` turns; `received` increments for `assistant` turns.
3. Character count per artifact: `len(Content)` for `Text` and `Reasoning`, `len(LLMString())` for `ToolCall` and `ToolResult`, `len(URL)` for `Image`, `0` for `Usage`, and JSON fallback for unknown types.
4. The telemetry package accepts a `metric.Meter` via constructor and is a no-op when `meter` is nil.
5. Examples (`tui-chat`, `http-chat`) demonstrate wiring a `service.version` OTel resource attribute via `sdkresource.New`.
6. [inferred] `x/telemetry` is added as a workspace module in `go.work`.
7. [inferred] `x/usage` documentation is updated to note the future deprecation consideration of `PropertiesEvent` in favor of telemetry metrics.

## Task Breakdown

### Task 1: Create `x/telemetry` Package with Core Metrics Logic
- **Goal**: Implement the `x/telemetry` package that provides an `OnEmit` callback recording per-artifact, per-role character counts via OTel counters.
- **Dependencies**: None.
- **Files Affected**: `go.work`
- **New Files**:
  - `x/telemetry/go.mod`
  - `x/telemetry/telemetry.go`
  - `x/telemetry/telemetry_test.go`
  - `x/telemetry/doc.go`
- **Interfaces**:
  - `func New(meter metric.Meter) *Telemetry` — constructor, nil-safe
  - `func (t *Telemetry) OnEmit() loop.OnEmit` — returns a callback for `loop.WithOnEmit`
- **Validation**:
  - `cd x/telemetry && go test -race ./...` passes.
  - `go vet ./x/telemetry/...` clean.
  - The package compiles without errors when integrated.
- **Details**:
  1. Create `x/telemetry/go.mod` with dependencies on `github.com/andrewhowdencom/ore`, `go.opentelemetry.io/otel/metric`, and `go.opentelemetry.io/otel/attribute`.
  2. Implement `Telemetry` struct holding `metric.Int64Counter` references for `ore.llm.characters.sent` and `ore.llm.characters.received`.
  3. Implement `countChars(art artifact.Artifact) int64` helper with type switch over `Text`, `Reasoning`, `ToolCall`, `ToolResult`, `Image`, `Usage`, and default JSON fallback.
  4. Implement `OnEmit()` method that type-asserts `TurnCompleteEvent`, selects `sent` or `received` counter based on `turn.Role`, iterates artifacts, computes `countChars`, and calls `counter.Add(ctx, n, attrs...)` with `attribute.String("artifact.kind", art.Kind())` and `attribute.String("role", string(turn.Role))`.
  5. If `meter` is nil, the constructor returns a `Telemetry` whose `OnEmit()` returns a no-op callback.
  6. Write table-driven tests covering: `Text`/`Reasoning` content length, `ToolCall`/`ToolResult` `LLMString()` length, `Image` URL length, `Usage` zero, unknown type JSON fallback, nil meter no-op, and per-role counter selection.
  7. Add `x/telemetry` to `go.work`.

### Task 2: Wire Telemetry into `examples/tui-chat` and Add Version Resource Attribute
- **Goal**: Update the TUI example to import `x/telemetry`, create a real OTel meter provider with a `service.version` resource attribute, and wire the telemetry callback into the `loop.Step`.
- **Dependencies**: Task 1.
- **Files Affected**: `examples/go.mod`, `examples/go.sum`, `examples/tui-chat/main.go`
- **New Files**: None.
- **Interfaces**: No new framework interfaces; example-level wiring only.
- **Validation**:
  - `cd examples && go build ./tui-chat/...` passes.
  - `cd examples && go vet ./tui-chat/...` clean.
  - `cd examples && go test -race ./tui-chat/...` passes (if tests exist).
- **Details**:
  1. Add `github.com/andrewhowdencom/ore/x/telemetry` as a `replace` and `require` in `examples/go.mod`.
  2. Add OTel SDK dependencies to `examples/go.mod`: `go.opentelemetry.io/otel/sdk/metric`, `go.opentelemetry.io/otel/sdk/resource`, `go.opentelemetry.io/otel/semconv/v1.26.0`.
  3. In `examples/tui-chat/main.go`, read `APP_VERSION` from environment (default `"dev"`).
  4. Create a `sdkmetric.MeterProvider` with an `sdkresource.Resource` containing `semconv.ServiceVersionKey.String(version)`.
  5. Pass `meter` to `telemetry.New(meter)`.
  6. Wire `telemetry.OnEmit()` via `loop.WithOnEmit(telemetry.OnEmit())` in the `stepFactory` alongside existing `loop.WithHandlers(usage.New())`.
  7. Keep the existing `noop.NewTracerProvider()` for tracing (the issue does not require switching to a real tracer provider).
  8. Run `go mod tidy` in `examples/` to update `go.sum`.

### Task 3: Wire Telemetry into `examples/http-chat` and Add Version Resource Attribute
- **Goal**: Update the HTTP example with the same telemetry and version wiring as the TUI example.
- **Dependencies**: Task 1, Task 2 (can be done in parallel with Task 2 if both examples are updated independently, but sequential is safer for validation).
- **Files Affected**: `examples/go.mod`, `examples/go.sum`, `examples/http-chat/main.go`
- **New Files**: None.
- **Interfaces**: None.
- **Validation**:
  - `cd examples && go build ./http-chat/...` passes.
  - `cd examples && go vet ./http-chat/...` clean.
- **Details**:
  1. In `examples/http-chat/main.go`, apply the same pattern as Task 2: read `APP_VERSION`, create meter provider with `service.version` resource, pass meter to `telemetry.New()`, wire `loop.WithOnEmit(telemetry.OnEmit())` in the `stepFactory`.
  2. Ensure `examples/go.mod` and `examples/go.sum` are already updated by Task 2 (no additional changes needed if Task 2 added the deps).

### Task 4: Update `x/usage` Documentation to Note Future Deprecation
- **Goal**: Add a note in `x/usage/doc.go` that `PropertiesEvent` may be deprecated in favor of `x/telemetry` metrics in future framework versions.
- **Dependencies**: Task 1.
- **Files Affected**: `x/usage/doc.go`
- **New Files**: None.
- **Interfaces**: None.
- **Validation**:
  - `cd x/usage && go test -race ./...` passes (no functional changes).
- **Details**:
  1. Append a paragraph to `x/usage/doc.go` noting that `x/telemetry` provides per-artifact OTel metrics and that `PropertiesEvent` may be deprecated in the future. Emphasize that `PropertiesEvent` is still required for TUI status bar rendering until the TUI is updated to consume telemetry metrics directly.

### Task 5: Add `x/telemetry` to `go.work` and Verify Workspace Integrity
- **Goal**: Ensure the new module is discoverable by the Go workspace and all cross-module references resolve.
- **Dependencies**: Task 1, Task 2, Task 3, Task 4.
- **Files Affected**: `go.work`
- **New Files**: None.
- **Validation**:
  - `go work sync` completes without errors.
  - `go test -race ./...` from the workspace root passes (or at least the modules that have tests).
- **Details**:
  1. Add `./x/telemetry` to the `use` block in `go.work` (if not already done in Task 1).
  2. Run `go work sync` to ensure all `replace` directives and module versions are consistent.
  3. Run `go test ./x/telemetry/...` and `go test ./examples/...` from the workspace root to verify integration.

## Dependency Graph

- Task 1 → Task 2 (Task 2 depends on the `x/telemetry` package existing)
- Task 1 → Task 3 (Task 3 depends on the `x/telemetry` package existing)
- Task 1 → Task 4 (Task 4 references the new package in docs)
- Task 2 → Task 5 (Task 5 verifies workspace after all changes)
- Task 3 → Task 5
- Task 4 → Task 5
- Task 2 || Task 3 (parallelizable once Task 1 is done)

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| `go.opentelemetry.io/otel/sdk/metric` API changes between versions and the examples fail to compile | Medium | Low | Pin to the same OTel version family as existing tracing deps (v1.44.0); use the stable `sdkmetric.NewMeterProvider` API which is unlikely to change. |
| `examples/go.mod` dependency resolution conflicts when adding `x/telemetry` and OTel SDK | Medium | Low | Use `go mod tidy` and `go work sync` after adding deps; the examples module is isolated from the root module. |
| `countChars` helper produces misleading counts for `Image` (URL length ≠ actual image bytes) | Low | Medium | Document that `Image` counts URL characters, not image bytes, in `x/telemetry/doc.go`. This is intentional: it reflects what the provider sees in the message payload. |
| `x/usage` consumers (TUI) break if `PropertiesEvent` is removed prematurely | High | Low | The plan explicitly does **not** remove `x/usage` or `PropertiesEvent`; it only adds a deprecation note. The TUI continues to receive `PropertiesEvent` from `x/usage`. |
| `LLMString()` for `ToolResult` includes JSON wrapper overhead, inflating counts vs. raw `Content` | Low | Medium | The issue explicitly recommends `LLMString()` as the count basis because it reflects what the provider actually sees. Document this choice in `doc.go`. |

## Validation Criteria

- [ ] `x/telemetry` package compiles and passes `go test -race ./...` with table-driven tests covering all artifact types and role mappings.
- [ ] `examples/tui-chat` compiles with `go build ./tui-chat/...` after adding telemetry and version wiring.
- [ ] `examples/http-chat` compiles with `go build ./http-chat/...` after adding telemetry and version wiring.
- [ ] `go work sync` completes without errors and the workspace is healthy.
- [ ] `x/usage/doc.go` contains a deprecation consideration note referencing `x/telemetry`.
- [ ] The telemetry `OnEmit` callback is a no-op when `meter` is nil (no runtime errors if the application does not configure OTel metrics).
- [ ] The telemetry package does not introduce any new dependencies to the root `github.com/andrewhowdencom/ore` module (only `go.opentelemetry.io/otel/metric` in the `x/telemetry` module).
