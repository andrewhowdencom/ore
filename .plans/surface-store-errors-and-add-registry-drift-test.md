# Plan: Surface store errors and add registry drift test

## Objective

Replace the silent `Store.Get` / `Store.GetBy` (`(*Thread, bool)`) contract with an error-returning contract that distinguishes "thread missing" from "thread file corrupt". Move artifact registration into self-registration via `init()` in `ore/artifact` so that adding a new persistable artifact type is mechanically paired with registering it. Add a drift-detection test that fails the build if the registered set and the package's persistable types diverge. Register the three currently-unregistered persistable kinds (`StopReason`, `ReasoningSignature`, `Compaction`) so the 27 affected threads in `~/.local/share/workshop/threads/` load cleanly.

## Context

`ore/session/serialize.go` defines a hand-maintained `artifactRegistry` that is consulted by `unmarshalArtifacts` during JSON deserialization of stored threads. The registry is missing three persistable kinds (`stop_reason`, `reasoning_signature`, `compaction`) that are defined in `ore/artifact/artifact.go` but never registered. When a stored thread contains any of these kinds, `unmarshalArtifacts` returns `unknown artifact kind "X"`, and `JSONStore.Get` silently swallows the error and returns `(nil, false)`. The end result is that a present, well-formed JSON file is reported to the user as "thread not found".

This was reproduced against `workshop`'s TUI: any session that emits an Anthropic extended-thinking response writes a `reasoning_signature` artifact, and any session that uses a tool calls and returns a `stop_reason` artifact with `reason: "tool_use"`. Both are emitted by the producer and required by the consumer, but cannot be round-tripped.

Affected files in this repo:

- `ore/session/store.go` — the `Store` interface
- `ore/session/json.go` — `JSONStore.Get`, `JSONStore.GetBy`
- `ore/session/memory.go` — `MemoryStore.Get`, `MemoryStore.GetBy`
- `ore/session/serialize.go` — `unmarshalArtifacts`, `dereferenceArtifact`, the central `artifactRegistry` map
- `ore/session/manager.go` — `Manager.Attach`, `Manager.Get`, `Manager.GetBy`
- `ore/artifact/artifact.go` — concrete types and the new `Persistent` marker / `Register` API
- `ore/x/conduit/tui/tui.go` — `Start` method calls `mgr.Attach`
- `ore/x/conduit/stdio/stdio.go` — calls `mgr.Attach`
- `ore/x/conduit/slack/thread.go` — calls `mgr.GetBy` then `mgr.Attach`
- `ore/session/manager_test.go` — `errStore` mock
- `ore/session/stream_test.go` — `saveErrStore` mock
- `ore/session/json_test.go`, `memory_test.go`, `integration_test.go` — many call sites destructure the `bool`
- `ore/x/conduit/http/handler_test.go` — `inner` mock embeds a `Store`
- `ore/artifact/artifact_test.go` (new test added here)

`ore/AGENTS.md` is decisive on direction: aggressive refactoring, no backwards-compatibility shims, `fmt.Errorf("...: %w", err)` for error wrapping, table-driven tests, `go test -race ./...`.

## Architectural Blueprint

**Contract change (Store interface):**

- `Get(id string) (*Thread, error)` — returns `ErrThreadNotFound` if absent, `ErrThreadCorrupt` (wrapping the underlying error) if the file is present but unparseable, `nil` on success.
- `GetBy(key, value string) (*Thread, error)` — symmetric: `ErrThreadNotFound` for no match, `ErrThreadCorrupt` (wrapping) if any thread file encountered during the scan fails to parse, `nil` on success.
- Drop the `bool` return entirely. No `GetErr` / `GetByErr` shims.

**Sentinel errors (`ore/session/errors.go`, new file):**

- `var ErrThreadNotFound = errors.New("thread not found")`
- `type ErrThreadCorrupt struct { Path string; Err error }` (or a sentinel + wrapping, whichever fits the existing conventions best — see Phase 5 risk on this choice).

**Artifact registry location and shape:**

- Move the registry data and `Register(kind, factory)` into `ore/artifact` (cohesion: registration is a property of types living there).
- Add a `Persistent` marker interface in `ore/artifact` with an unexported method `isPersistent()`. Same sealing trick AGENTS.md describes for `Artifact` — but inverted: `Artifact` uses public methods for cross-package extensibility; `Persistent` uses an unexported method so only types within `ore/artifact` can implement it.
- Each concrete persistable type's file gains an `init()` block calling `Register(kind, factory)` and an `isPersistent()` method on the value receiver.
- `ore/session/serialize.go` no longer owns the central map; `unmarshalArtifacts` calls `artifact.Registered()` (returns a copy of the map) to look up factories.
- `dereferenceArtifact` is extended to handle the newly-registered value types so the round-trip contract from #416 continues to hold.

**Drift detection (in `ore/artifact`, new test):**

- A package-level slice `allPersistent` of zero-value instances of every concrete `Persistent` type. Single source of truth for "these are the persistable kinds".
- `init()` in `ore/artifact` (separate from per-type `init()`s) iterates `allPersistent`, registers each.
- The drift test asserts that `Registered()`'s key set equals `allPersistent`'s `Kind()` set (computed dynamically). Fail message lists any divergence.
- Adding a new persistable type requires (a) declaring it, (b) implementing `Artifact`, (c) implementing `Persistent` (which forces the unexported method, which forces an edit in `ore/artifact`), (d) adding an entry to `allPersistent`. Forgetting (d) fails the drift test at `go test`.

**Tradeoffs explicitly evaluated:**

- *Registry location (artifact vs. session):* chose `ore/artifact` for cohesion. `init()` blocks live next to type definitions; the registry is consumed by `ore/session` but isn't owned by it. Cost: `ore/artifact` gains state (a `map` and a `sync.Mutex` for thread-safe `Register`).
- *Marker interface vs. allowlist:* chose marker interface so the compiler enforces "must be in `ore/artifact`" via the unexported method. An allowlist string slice would be order-sensitive and easier to forget.
- *Thread-safe Register:* added because tests in other packages may construct types in parallel; cheap, prevents future flakiness.
- *Backward compat:* deliberately broken per AGENTS.md. Workshop will need a bumped ore dependency after this lands.

## Requirements

1. `session.Store.Get` and `session.Store.GetBy` both return `(*Thread, error)` per the contract above. No other Store methods change signature.
2. `ErrThreadNotFound` and `ErrThreadCorrupt` are exported from `ore/session`. Callers can use `errors.Is` to distinguish.
3. `JSONStore` distinguishes `os.ReadFile` returning `os.ErrNotExist` (→ `ErrThreadNotFound`) from any other failure (→ `ErrThreadCorrupt` wrapping).
4. `MemoryStore.Get` and `MemoryStore.GetBy` return `ErrThreadNotFound` for misses.
5. `ore/artifact` exports `Register(kind string, factory func() Artifact)`, `Registered() map[string]func() Artifact`, and a `Persistent` marker interface. The map is populated only via `Register`; reads via `Registered` return a copy.
6. Every concrete persistable type in `ore/artifact` self-registers via `init()`. Specifically: `Text`, `ToolCall`, `ToolResult`, `Usage`, `Image`, `Reasoning`, `StopReason`, `ReasoningSignature`, `Compaction`.
7. Delta kinds (`TextDelta` and any future `*Delta`) do **not** implement `Persistent` and do not register. They remain ephemeral by construction.
8. `unmarshalArtifacts` in `ore/session/serialize.go` looks up factories via `artifact.Registered()`. `dereferenceArtifact` covers all registered value types.
9. All production callers — `Manager.Attach`, `Manager.Get`, `Manager.GetBy`, `x/conduit/tui.Start`, `x/conduit/stdio` start path, `x/conduit/slack.resolveThread` — propagate the new error meaningfully. Conduits surface "thread not found" vs "thread file corrupt" distinctly to the user.
10. All test mocks (`errStore` in `manager_test.go`, `saveErrStore` in `stream_test.go`, `inner` in `x/conduit/http/handler_test.go`, the `inner` mock in `stream_test.go`) match the new signature.
11. Round-trip tests cover a thread containing `StopReason`, `ReasoningSignature`, and `Compaction` artifacts (in addition to the existing coverage).
12. A drift-detection test in `ore/artifact` fails the build if the registered set diverges from `allPersistent`.
13. `go test -race ./...` passes for `ore/session`, `ore/artifact`, `ore/x/conduit/tui`, `ore/x/conduit/stdio`, `ore/x/conduit/slack`, `ore/x/conduit/http`.

## Task Breakdown

### Task 1: Add registry infrastructure to `ore/artifact`

- **Goal**: Establish the foundational API in `ore/artifact`: `Register`, `Registered`, and the `Persistent` marker interface. No type registration yet.
- **Dependencies**: None.
- **Files Affected**: `/home/andrewhowdencom/Development/ore/artifact/artifact.go`
- **New Files**: None.
- **Interfaces**: `type Persistent interface { Artifact; isPersistent() }`. `func Register(kind string, factory func() Artifact)`. `func Registered() map[string]func() Artifact`.
- **Validation**: `go build ./ore/artifact/...` passes. Existing artifact tests pass. `Register` is thread-safe.
- **Details**: Add a package-level `sync.Mutex`-guarded map and the three exported symbols. The unexported `isPersistent()` method is the only way to implement `Persistent`. The `Registered()` function returns a copy so callers can't mutate the map.

### Task 2: Self-register the six currently-registered kinds

- **Goal**: Move registration of `Text`, `ToolCall`, `ToolResult`, `Usage`, `Image`, `Reasoning` from the central `artifactRegistry` map in `ore/session/serialize.go` into per-type `init()` blocks in `ore/artifact`.
- **Dependencies**: Task 1.
- **Files Affected**: `/home/andrewhowdencom/Development/ore/artifact/artifact.go`, `/home/andrewhowdencom/Development/ore/session/serialize.go`
- **New Files**: None.
- **Interfaces**: Each of the six types implements `Persistent` (i.e. gains an unexported `isPersistent()` method on its value receiver) and has an `init()` calling `Register(...)`.
- **Validation**: `go test ./ore/session/...` round-trip tests pass (e.g. `TestMarshalArtifacts_AllTypes` if it exists). Removing the central map and consulting `artifact.Registered()` instead produces identical behavior.
- **Details**: The central `artifactRegistry` map in `serialize.go` is removed. `unmarshalArtifacts` calls `artifact.Registered()` once at the top of the function (or once at package init, since the registry doesn't change at runtime) to obtain the lookup map.

### Task 3: Register the three missing kinds

- **Goal**: Add `init()` blocks for `StopReason`, `ReasoningSignature`, `Compaction` so they register automatically. Add `allPersistent` slice containing all nine persistable kinds.
- **Dependencies**: Task 2.
- **Files Affected**: `/home/andrewhowdencom/Development/ore/artifact/artifact.go`
- **New Files**: None.
- **Interfaces**: Three additional types implement `Persistent`. `allPersistent` slice in `ore/artifact` contains one entry per persistable type (zero-value instance or factory).
- **Validation**: A test thread containing all three kinds round-trips cleanly. The 27 affected threads in `~/.local/share/workshop/threads/` load without error.
- **Details**: A package-level `init()` in `ore/artifact` iterates `allPersistent` and registers each, providing a single point where the slice is used. `dereferenceArtifact` in `serialize.go` is extended with cases for the three new value types so the dereference contract holds.

### Task 4: Change `Store` interface signatures

- **Goal**: Update `Store.Get` and `Store.GetBy` to return `(*Thread, error)`. Implement the new contract in `JSONStore` and `MemoryStore`. Add `ErrThreadNotFound` and `ErrThreadCorrupt` sentinels.
- **Dependencies**: Tasks 2, 3 (registry must be ready because some callers will need to surface "kind X unknown" errors distinctly).
- **Files Affected**:
  - `/home/andrewhowdencom/Development/ore/session/store.go`
  - `/home/andrewhowdencom/Development/ore/session/json.go`
  - `/home/andrewhowdencom/Development/ore/session/memory.go`
  - `/home/andrewhowdencom/Development/ore/session/errors.go` (new)
- **New Files**: `/home/andrewhowdencom/Development/ore/session/errors.go`.
- **Interfaces**:
  - `var ErrThreadNotFound = errors.New("thread not found")`
  - `var ErrThreadCorrupt = errors.New("thread file corrupt")` — used as a sentinel; the actual error returned wraps the underlying cause via `fmt.Errorf("thread %s: %w: %w", id, ErrThreadCorrupt, cause)` (Go 1.20+ multi-`%w`).
- **Validation**: `go build ./...` succeeds for `ore/session` and `ore/artifact`. Existing round-trip tests pass with adjusted assertions. A unit test confirms `JSONStore.Get("missing")` returns `ErrThreadNotFound` and `JSONStore.Get("corrupt-but-present")` returns an error satisfying `errors.Is(err, ErrThreadCorrupt)`.
- **Details**: `JSONStore.Get` distinguishes `errors.Is(err, os.ErrNotExist)` → `ErrThreadNotFound`, else → `ErrThreadCorrupt` wrapping. `JSONStore.GetBy` propagates the first `ErrThreadCorrupt` it encounters, or returns `ErrThreadNotFound` if the scan completes with no match. `MemoryStore.Get`/`GetBy` return `ErrThreadNotFound` for misses and `nil` for hits. The build will be broken in callers until Task 5 — that's expected, Task 5 is the next task.

### Task 5: Update Manager and all conduit callers

- **Goal**: `Manager.Attach`, `Manager.Get`, `Manager.GetBy` propagate the new error. All production conduits (`tui`, `stdio`, `slack`) propagate the error or, where appropriate, distinguish "not found" vs "corrupt" in user-facing messages.
- **Dependencies**: Task 4.
- **Files Affected**:
  - `/home/andrewhowdencom/Development/ore/session/manager.go`
  - `/home/andrewhowdencom/Development/ore/x/conduit/tui/tui.go`
  - `/home/andrewhowdencom/Development/ore/x/conduit/stdio/stdio.go`
  - `/home/andrewhowdencom/Development/ore/x/conduit/slack/thread.go`
- **New Files**: None.
- **Interfaces**: `Manager.Attach(threadID string) (*Stream, error)` (already returns error). `Manager.Get(threadID string) (*Thread, error)`. `Manager.GetBy(key, value string) (*Thread, error)`.
- **Validation**: `go build ./...` succeeds across all ore modules. Existing tests for the conduits pass.
- **Details**: TUI surfaces `ErrThreadNotFound` to the user with a "thread not found" message and `ErrThreadCorrupt` with a "thread file is corrupt: <cause>" message including the underlying error. Slack conduit propagates the error to its caller (which already wraps with `fmt.Errorf`). Stdio propagates the error through the existing error-returning start path.

### Task 6: Update test mocks

- **Goal**: All test mocks implementing `Store` match the new `(*Thread, error)` signature.
- **Dependencies**: Task 5.
- **Files Affected**:
  - `/home/andrewhowdencom/Development/ore/session/manager_test.go` (`errStore` mock and other test-only `Store` implementations)
  - `/home/andrewhowdencom/Development/ore/session/stream_test.go` (`saveErrStore` mock, plus direct `store.Get(...)` callers that destructure `bool`)
  - `/home/andrewhowdencom/Development/ore/x/conduit/http/handler_test.go` (mock with `inner` field that delegates to a `Store`)
  - `/home/andrewhowdencom/Development/ore/session/json_test.go` (~8 call sites that destructure the `bool` return)
  - `/home/andrewhowdencom/Development/ore/session/memory_test.go` (~3 call sites that destructure the `bool` return)
  - `/home/andrewhowdencom/Development/ore/session/integration_test.go` (1 call site)
  - `/home/andrewhowdencom/Development/ore/x/conduit/tui/tui_test.go` and `/home/andrewhowdencom/Development/ore/x/conduit/stdio/stdio_test.go` (if they have direct `Store` consumers)
- **New Files**: None.
- **Validation**: `go test ./...` passes for all ore modules. Existing test assertions on the boolean return are updated to assert on the error return (e.g. `_, err := store.Get("missing"); !errors.Is(err, ErrThreadNotFound)`).
- **Details**: Mocks that previously returned `(nil, false)` now return `(nil, ErrThreadNotFound)`. Mocks that delegated to `s.inner.Get` propagate the inner error.

### Task 7: Add drift-detection test in `ore/artifact`

- **Goal**: A test that verifies the registered kinds set equals the kinds produced by `allPersistent`. Adding a new persistable type without updating `allPersistent` fails this test.
- **Dependencies**: Task 3 (the `allPersistent` slice must exist).
- **Files Affected**: `/home/andrewhowdencom/Development/ore/artifact/artifact.go`
- **New Files**: `/home/andrewhowdencom/Development/ore/artifact/drift_test.go`
- **Interfaces**: None (test only).
- **Validation**: `go test -race ./ore/artifact/...` passes. The test prints the registered kinds on success. On failure, it lists which kinds are registered but not in `allPersistent` and which are in `allPersistent` but not registered.
- **Details**: Table-driven test. Iterates `artifact.Registered()` and asserts each factory's `Kind()` matches its key. Iterates `allPersistent`, constructs each instance, calls `Kind()`, and asserts the resulting set equals the keys of `Registered()`. Fail message names the diverging kinds explicitly.

### Task 8: Add round-trip regression test for the three new kinds

- **Goal**: A regression test that constructs a thread containing `StopReason`, `ReasoningSignature`, and `Compaction` artifacts, saves it via `JSONStore.Save`, reloads via `JSONStore.Get`, and verifies the artifacts round-trip without error.
- **Dependencies**: Task 6 (test mocks must compile).
- **Files Affected**: `/home/andrewhowdencom/Development/ore/session/json_test.go` (or `serialize_test.go`).
- **New Files**: None (added to an existing test file).
- **Interfaces**: None (test only).
- **Validation**: `go test -race ./ore/session/...` passes. The new test would have failed before Tasks 2 and 3; it passes after.
- **Details**: Constructs a `Thread` with one `Turn` containing all three new artifact kinds. Saves to a `t.TempDir()`-backed `JSONStore`. Re-loads via `Get` and asserts no error and all artifacts present.

### Task 9: Validate end-to-end against affected workshop threads

- **Goal**: Confirm the user's reproduction case (`workshop --thread a907352288655004dee7a7495dad147e`) loads successfully after these changes are released into workshop's ore dependency.
- **Dependencies**: Tasks 1–8 complete and an ore release cut.
- **Files Affected**: None in this repo. In workshop, `go.mod` will need a bumped `ore/session` reference (separate concern, called out as a risk below).
- **Validation**: `workshop thread export a907352288655004dee7a7495dad147e` succeeds (text dump shows the conversation). `workshop --thread a907352288655004dee7a7495dad147e` opens the TUI with the conversation history visible. None of the 27 affected threads in `~/.local/share/workshop/threads/` report "thread not found" from `workshop thread export`.
- **Details**: This task is a manual validation step, not code. It exists so the plan is verifiably "done" rather than merely "compiles".

## Dependency Graph

- Task 1 → Task 2 → Task 3 → Task 4 → Task 5 → Task 6
- Task 7 depends on Task 3 (needs `allPersistent`)
- Task 8 depends on Task 6 (needs all mocks compiling)
- Task 9 depends on Tasks 1–8 + an ore release

Tasks 1–6 are strictly sequential. Tasks 7 and 8 can run in parallel with each other after their respective dependencies are met, but each is small and naturally folds into the same commit. Task 9 is post-merge.

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Workshop's `go.mod` references tagged ore versions (`ore v0.12.0`, etc.); main-branch changes won't reach workshop until a release is cut and bumped | High (user's repro can't be validated until then) | High | Task 9 calls this out as an explicit step. Builder should tag a new ore release (e.g. `ore/v0.13.0`) and bump workshop's `go.mod` references as the final action. |
| `allPersistent` slice drifts from the set of persistable types if someone adds a type without editing the slice | Medium (drift test catches it, but only on next test run) | Medium | Drift test (Task 7) produces an explicit failure message naming the missing/extra kinds. AGENTS.md test policy (`go test -race ./...`) makes this a hard wall. |
| Goroutine-safety regression in `Register()` if called from a non-`init()` context | Low (would manifest as flaky tests) | Low | Add `sync.Mutex` guarding the map. The drift test runs with `-race`. |
| `ErrThreadCorrupt` sentinel shape: choosing a struct vs. a wrapped sentinel changes caller ergonomics | Low | Low | Default to `var ErrThreadCorrupt = errors.New(...)` + `fmt.Errorf("...: %w", ErrThreadCorrupt)` + `fmt.Errorf("...: %w", underlyingErr)` chained via multi-`%w` (Go 1.20+). Confirmed compatible with the existing workshop code path. |
| `Persistent` sealed by unexported method means external artifact types (per AGENTS.md, allowed via public `Artifact` interface) cannot self-register | Medium (limits extensibility) | Low | By design. External artifact types are either ephemeral (don't need persistence) or must contribute a `Persistent` shim that delegates to a registered kind. This matches the current implicit behavior — external types already couldn't register via the central map. |
| `init()` ordering across packages: if any non-init caller reads `Registered()` before all packages' `init()` blocks have run | High (empty registry → all unmarshal fails) | Very Low | Go guarantees `init()` in imported packages completes before the importing package's `init()` runs. Since `ore/session` imports `ore/artifact`, the registry is populated before any `ore/session` code runs. No runtime check needed. |
| Reflection-based enumeration of `allPersistent` types is not used (intentional); adding a new type to `ore/artifact` is a multi-step process | Low (friction) | Certain | Documented in Task 1's `Persistent` doc comment. Trade-off accepted: avoids `go/types` dependency for one test. |

## Validation Criteria

- [ ] `go test -race ./ore/artifact/...` passes.
- [ ] `go test -race ./ore/session/...` passes.
- [ ] `go test -race ./ore/x/conduit/tui/...` passes.
- [ ] `go test -race ./ore/x/conduit/stdio/...` passes.
- [ ] `go test -race ./ore/x/conduit/slack/...` passes.
- [ ] `go test -race ./ore/x/conduit/http/...` passes.
- [ ] `go vet ./...` clean across all updated modules.
- [ ] Drift test fails if a new persistable type is added without updating `allPersistent`.
- [ ] Round-trip test covers `StopReason`, `ReasoningSignature`, `Compaction` artifacts (would fail before Tasks 2–3, passes after).
- [ ] `JSONStore.Get("nonexistent")` returns an error satisfying `errors.Is(err, session.ErrThreadNotFound)`.
- [ ] `JSONStore.Get` against a file containing an unknown artifact kind returns an error satisfying `errors.Is(err, session.ErrThreadCorrupt)` with the underlying kind name in the wrapped cause.
- [ ] Workshop's `cmd/workshop/thread.go` `runThreadExport` propagates the new error to the user (so `workshop thread export <bogus-uuid>` produces a meaningful error rather than a stack trace).
- [ ] (Post-release) `workshop thread export a907352288655004dee7a7495dad147e` succeeds and shows the conversation.
- [ ] (Post-release) `workshop --thread a907352288655004dee7a7495dad147e` opens the TUI with the conversation history loaded.