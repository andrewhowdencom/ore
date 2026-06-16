# Plan: Fix TUI startup hang from status-bar seed sent before event loop

## Objective

Resolve a regression introduced by commit `cf88e66` (`.plans/fix-tui-status-bar-on-startup.md`) where `TUI.Start` in `ore/x/conduit/tui/tui.go` invokes `t.program.Send(statusMsg{...})` *before* `p.Run()` is reached. The send blocks inside `Program.Send`'s internal `select` (waiting for the Bubble Tea event loop to drain its message channel), but the loop has not started — and because the goroutine spawns and `p.Run()` come *after* the send, they are unreachable. The process stays alive (pprof, GC, signal handlers all keep running) but the TUI never renders. Confirming evidence is the captured stack dump: the main goroutine is parked inside `Program.Send` at `tui.go:260`.

The fix is to route the status-bar seed through `m.Init()`'s `tea.Cmd` pathway, so it reaches the existing `statusMsg` handler through the normal message channel *after* the event loop is running. The handler's full side-effect path (merge into `m.status`, `m.recalcLayout()`, `m.syncViewport()`, viewport `GotoBottom`) is preserved, so we do not introduce a second code path for status updates.

## Context

### Repository topology

The fix is contained in a single sub-module:

- **`ore/x/conduit/tui/`** — Bubble Tea TUI conduit. Module: `charm.land/bubbletea/v2 v2.0.6`; the workshop pin uses `v2.0.7`. No version change is required for this fix.
- **Workshop** consumes the module via `workshop/go.mod` (line 47, `charm.land/bubbletea/v2 v2.0.7 // indirect`). Workshop changes are out of scope; a downstream `go get` + `go mod tidy` will follow the `ore` release.

### Key files and line ranges (observed in this session)

- `ore/x/conduit/tui/tui.go`
  - `Start` runs L233-333. Relevant landmarks in the current (broken) source:
    - L250-251: `m := t.initModel(surfEventsCh, stream); p := tea.NewProgram(&m)`
    - L253: `t.program = p`
    - L255-261: the broken comment block plus `if msg := statusFromStream(stream); msg != nil { t.program.Send(msg) }`
    - L263-265: `outputCh := stream.Subscribe(...)`
    - L267-291: output-event goroutine (range + `t.program.Send(...)`)
    - L294-318: user-event goroutine
    - L321-326: ctx-cancellation goroutine (`<-ctx.Done(); p.Quit()`)
    - L328: `_, err = p.Run()` — the call we never reach with the bug present
  - `initModel` (L184-208) returns a freshly-built `model` and calls `m.loadHistory(stream.Turns())` at L206. It has the `*session.Stream` in hand, so it can resolve the seed at construction time.
  - `statusFromStream` (L221-228) is a package-private helper that already encapsulates the "build a `statusMsg` from `stream.AllMetadata()` if any, else `nil`" contract. It is exercised by `TestStatusFromStream` (L146-211) and can be reused as-is.
- `ore/x/conduit/tui/model.go`
  - `model` struct (L140-...) holds `status map[string]string` at L163 alongside other persisted UI state.
  - `Init` (L445-447) currently returns `nil`. It is called once by Bubble Tea at the start of the event loop, before the first user input is processed.
  - `case statusMsg:` (L559-571) is the merge handler. It does: capture `wasAtBottom`, lazily init `m.status`, merge key-by-key, `m.recalcLayout()`, `m.syncViewport()`, `GotoBottom` if at bottom. Skipping any of these is what the user described as "flaky."
- `ore/x/conduit/tui/tui_test.go`
  - Existing tests: `TestNew` (L42), `TestNew_WithThreadID` (L52), `TestStart_AttachNotFound` (L62), `TestNew_WithName` (L78), `TestTUI_ImplementsAudioNotifier` (L92), `TestTUI_InitModel_ResumesThreadWithHistory` (L96), `TestStatusFromStream` (L146).
  - The commit message of `cf88e66` claimed the integration path "is exercised by the existing TestStart_* tests." That is incorrect: `TestStart_AttachNotFound` short-circuits at `mgr.Attach` before reaching the broken `Send`. The `statusFromStream` helper has good unit coverage, but the call from `Start` has zero coverage. **This test gap is why the regression slipped in.**

### Why the original plan's mitigation was wrong

The prior plan's `Task 2` rationale said:

> *"Rationale for placement: the bootstrap must fire before the `Subscribe` call so it is processed by the Bubble Tea event loop in the same scheduling slice as the first live event."*

The author reasoned about *relative ordering against the live goroutine* but did not reason about the *absolute ordering against `p.Run()`*. The placement "after `t.program = p` and before `stream.Subscribe`" is between `NewProgram` (which does *not* start the loop) and `p.Run` (which *does*). That is the exact wedge we are stuck in today.

## Architectural Blueprint

A two-part change in one file group (`ore/x/conduit/tui/`), keeping the same architectural shape as the prior plan (one bulk accessor on the stream, one bootstrap call from the TUI). The difference is *when* the bootstrap is delivered.

1. **Resolve the seed in `initModel` and stash it on the model.** `initModel` already has the `*session.Stream` parameter. It calls the existing `statusFromStream` helper and stores the resulting `tea.Msg` (or `nil`) on a new package-private field `model.initStatusMsg`. No goroutines, no `Send`, no race.

2. **Dispatch the seed via `m.Init()`.** `m.Init()` currently returns `nil`. It is changed to return a `tea.Cmd` (a closure) that yields `m.initStatusMsg` if non-nil, else `nil`. Bubble Tea invokes `Init` once at the start of the event loop. The `tea.Cmd` runs in the loop, the resulting `statusMsg` is dispatched through the normal channel, and the existing `statusMsg` handler performs the full merge plus `recalcLayout`/`syncViewport`.

3. **Remove the broken `Send` from `TUI.Start`.** Delete the block at `tui.go:256-260` and its comment. The `statusFromStream` helper is no longer called from `Start`; it is called from `initModel` instead. The helper itself stays in the package (still used and still tested).

### Why this shape, in one line

We preserve the "send one `statusMsg` seeded from the stream" contract from the prior plan, but deliver it through the only path that actually works: a `tea.Cmd` returned from `Init`, which runs *inside* the event loop. No second code path for status updates is introduced; the merge logic stays in the existing handler.

### Why not the alternatives considered during ideation

- **Direct field seeding in `initModel`** (`m.status = stream.AllMetadata()` followed by manual `m.recalcLayout()` + `m.syncViewport()`). Rejected: the user identified this as "flaky" because the handler does more than the merge, and any future addition to the handler's side-effect list would silently bypass the seed. The commit message of `cf88e66` already rejected the "two paths to keep in sync" approach on the same grounds.
- **Defer `Send` to a goroutine that waits for "loop running"**. Rejected: requires a new synchronization primitive for a problem that has an idiomatic Bubble Tea solution (`Init` → `Cmd`).
- **Drop the seed and rely on the live `PropertiesEvent` flow**. Rejected: the user pointed out that TUI-owned metadata (`cwd`, `git_branch`, `tui.pid`) does not flow through `PropertiesEvent`. The status bar would be empty for the common case.

## Requirements

1. A new TUI session must show the seeded status-bar keys (`thread_id`, `cwd`, `git_branch`, `tui.pid`, and any `WithDefaultMetadata` keys) after the event loop starts. First-frame timing is not a hard requirement; what arrives on the loop's first dispatch of the seed message is sufficient. [explicit, from the user's confirmation that the timing constraint is loose]
2. The seed must be delivered through the existing `statusMsg` handler, not a direct field write, so the handler's `recalcLayout`/`syncViewport`/`GotoBottom` side effects are not skipped. [explicit, from the user's "flaky" framing]
3. Resuming a thread via `--thread <uuid>` must produce the same outcome. [inferred, mirrors the prior plan's requirement 2]
4. `TUI.Start` must not block before `p.Run()` is reached. The main goroutine must not be parked inside `Program.Send`. [explicit, from the stack dump]
5. The `Subscribe` contract must remain live-only; no event replay is reintroduced. [inferred, mirrors prior plan requirement 3]
6. The fix is scoped to `ore`; workshop bumps its `x/conduit/tui` pin in a follow-up release commit. [inferred from the existing release flow in `ore/loop`'s git history]
7. An integration test must exist that calls `Start` end-to-end and asserts the program actually reached the event loop. [explicit, from the test gap identified in context]

## Task Breakdown

### Task 1: Add regression test for `Start` end-to-end (TDD red)

- **Goal**: Pin the regression with a test that fails against the current `cf88e66` code and will pass after Task 2.
- **Dependencies**: None.
- **Files Affected**: `ore/x/conduit/tui/tui_test.go`
- **New Files**: None.
- **Interfaces**: No new exported types.
- **Validation**:
  - `go test -race ./x/conduit/tui/...` runs the new test; the new test **fails** with the current code (this is the deliberate "TDD red" state).
  - `go build ./...` and all other existing tests still pass.
  - The failing test reports a clear signal: "Start did not return within 2s after context cancellation; the Bubble Tea event loop likely never started."
- **Details**:
  - Add `TestStart_ReachesEventLoop` to `tui_test.go`. The test:
    1. Builds a `session.Manager` with a `MemoryStore`, a `mockProvider` (the existing test helper at L19), and `simpleProcessor()` (L36).
    2. Calls `New(mgr)` to construct a default TUI (no `WithThreadID`, so `Start` takes the `mgr.Create()` path).
    3. Spawns `c.Start(ctx)` in a goroutine and waits for the returned error on a buffered channel.
    4. Cancels the context immediately.
    5. Selects on `<-done` (with a 2-second timeout via `time.After`). Asserts the error is `nil` if `Start` returned in time; otherwise calls `t.Fatal` with a message that points at the symptom (the main goroutine stuck in `Program.Send`).
  - The 2-second timeout is generous on purpose: the test is *not* measuring performance, it is asserting liveness. A shorter timeout risks flakiness on slow CI; a longer timeout slows the failure case. 2 seconds is a reasonable middle ground for a regression test.
  - Use the same context-cancellation pattern as the existing `TestStart_AttachNotFound` (L62-76) so reviewers see a familiar shape. The difference is that this test expects `Start` to return *no error* after cancel, not the "thread not found" error.
  - Do **not** add correctness assertions about `m.status` in this test. That is covered by Task 2's unit test. Keeping the integration test focused on liveness avoids coupling it to model internals.
  - Concretely, the test body is shaped like:
    ```go
    func TestStart_ReachesEventLoop(t *testing.T) {
        store := session.NewMemoryStore()
        prov := &mockProvider{}
        mgr := session.NewManager(store, prov,
            func(*session.Stream) ([]loop.Option, error) { return nil, nil },
            simpleProcessor())

        c, err := New(mgr)
        require.NoError(t, err)

        ctx, cancel := context.WithCancel(context.Background())
        defer cancel()

        done := make(chan error, 1)
        go func() { done <- c.Start(ctx) }()

        cancel()
        select {
        case err := <-done:
            require.NoError(t, err, "Start should return cleanly when context is cancelled")
        case <-time.After(2 * time.Second):
            t.Fatal("Start did not return within 2s after context cancellation; " +
                "the Bubble Tea event loop likely never started")
        }
    }
    ```
  - This test will hang for 2 seconds and then fail when run against the current code. That is the intended signal: the regression is reproduced and pinned.

### Task 2: Route the seed through `m.Init()` and remove the broken `Send` (TDD green)

- **Goal**: Eliminate the `Send`-before-`Run` deadlock by delivering the status-bar seed through `m.Init()`'s `tea.Cmd`, and remove the broken call from `TUI.Start`.
- **Dependencies**: Task 1.
- **Files Affected**:
  - `ore/x/conduit/tui/model.go` — add a new field on the `model` struct; modify `Init`.
  - `ore/x/conduit/tui/tui.go` — populate the new field in `initModel`; delete the broken `Send` block from `Start`.
  - `ore/x/conduit/tui/tui_test.go` — add unit tests for the seed wiring (one for `initModel`, one for `Init`).
- **New Files**: None.
- **Interfaces**:
  - New struct field on `model` (package-private):
    ```go
    // initStatusMsg is a one-shot status seed produced by initModel from
    // stream.AllMetadata(). It is yielded by Init() as a tea.Cmd so the
    // existing statusMsg handler can merge it into m.status through the
    // normal message channel — i.e., after the event loop has started.
    // It is never read after Init() runs and can otherwise be ignored.
    initStatusMsg tea.Msg
    ```
  - Modified `Init` signature (return type unchanged; body changes):
    ```go
    func (m *model) Init() tea.Cmd {
        if m.initStatusMsg == nil {
            return nil
        }
        seed := m.initStatusMsg
        return func() tea.Msg { return seed }
    }
    ```
    The closure captures `seed` by value so the returned `Cmd` does not depend on `m` after the program is constructed.
- **Validation**:
  - `go build ./...` from the `ore` module root.
  - `go test -race ./x/conduit/tui/...` passes. Specifically:
    - The new `TestStart_ReachesEventLoop` (Task 1) now passes — the event loop starts, the context-cancellation goroutine fires `p.Quit()`, `p.Run()` returns, `Start` returns `nil`.
    - The new `TestInitModel_SeedsStatusFromStream` (this task) passes — calling `initModel` on a stream with `thread_id` and `cwd` metadata produces a `model.initStatusMsg` that is a `statusMsg` carrying both keys.
    - The new `TestInit_DispatchesSeedCmd` (this task) passes — calling `m.Init()` on that model returns a non-nil `Cmd`; executing the `Cmd` (i.e., calling it as a function) returns the same `statusMsg`.
    - The existing `TestStatusFromStream` (L146-211) still passes — the helper itself is unchanged and still used by `initModel`.
    - The existing `TestTUI_InitModel_ResumesThreadWithHistory` (L96-144) still passes — it does not assert on `m.status`, so adding a field does not break it. The new field is set during `initModel` and the test does not observe it, but the test will not fail either.
    - All other existing tests (`TestNew*`, `TestStart_AttachNotFound`, `TestTUI_ImplementsAudioNotifier`, `TestModel_*` in `model_test.go`, `TestView_*` in `view_test.go`) still pass.
  - `go vet ./...` clean.
- **Details**:
  - **In `tui.go`**, in `initModel` (L184-208), after the existing `m.loadHistory(stream.Turns())` call (L206), add:
    ```go
    // Resolve the status-bar seed up front. It is delivered via Init()'s
    // tea.Cmd (not via a direct Send) so the message reaches the
    // statusMsg handler through the normal channel after the event loop
    // has started. statusFromStream returns nil if there is no metadata,
    // which Init() also treats as a no-op.
    m.initStatusMsg = statusFromStream(stream)
    ```
  - **In `tui.go`**, in `Start`, delete the block at L255-261 (the 4-line comment plus the `if … t.program.Send(msg)` 3-liner). The exact lines being removed are:
    ```go
    // Seed the status bar from the stream's current metadata before the
    // live-event goroutine starts. Default-metadata PropertiesEvents fire
    // during mgr.Create / mgr.Attach, before we can subscribe, so without
    // this seed the status bar would render empty on the first frame.
    if msg := statusFromStream(stream); msg != nil {
        t.program.Send(msg)
    }
    ```
    The `statusFromStream` helper (L221-228) and its package-private test coverage stay. It is now called from `initModel` rather than from `Start`.
  - **In `model.go`**, add the `initStatusMsg` field to the `model` struct. Place it after the `status map[string]string` field at L163, with a comment explaining its lifecycle (one-shot, set in `initModel`, consumed in `Init`).
  - **In `model.go`**, modify `Init` (L445-447) to return the seed `Cmd` as shown in the Interfaces block above.
  - **In `tui_test.go`**, add two unit tests near the existing `TestStatusFromStream`:
    - `TestInitModel_SeedsStatusFromStream` — table-driven, with two cases: (a) stream with `thread_id` + `cwd` set, expect `m.initStatusMsg` to be a `statusMsg` carrying both keys; (b) fresh stream with no metadata, expect `m.initStatusMsg` to be `nil`. Construct the `*TUI` directly (mirroring `TestTUI_InitModel_ResumesThreadWithHistory` at L96-144) and call `initModel` with a buffered `eventsCh` and the `*session.Stream`.
    - `TestInit_DispatchesSeedCmd` — build a model via `initModel` as above, call `m.Init()`, assert the returned `Cmd` is non-nil, execute it, assert the result is a `statusMsg` whose payload equals the stream's metadata. Repeat the nil case: a model with `initStatusMsg == nil` should produce `m.Init() == nil`.
  - These unit tests do not need a real Bubble Tea program; they are pure functions and are therefore fast and deterministic.
  - Do not modify the `statusFromStream` helper. It already implements the "return nil if no metadata" guard, which `Init` and `initModel` both rely on.
  - Do not add a new constructor option. The fix is internal; existing callers (`app.RunTUI` in `workshop/internal/app/app.go:268`, examples) need no changes.

## Dependency Graph

- Task 1 → Task 2 (Task 2 is the implementation that makes Task 1's test pass)
- No parallelizable work; the two tasks are sequential and form a single TDD cycle.

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| The new `initStatusMsg` field is read after `Init` and observed to be `nil` (e.g., by a future test or refactor) and someone concludes the seed "didn't work" | Low | Low | Document the field as one-shot in its comment. The `Init` closure captures the value by copy, so subsequent mutation of `m.initStatusMsg` cannot affect what the `Cmd` yields. |
| The `Init` closure accidentally captures `m` by reference and the field gets cleared before the `Cmd` runs | Medium | Low | Capture the local `seed := m.initStatusMsg` before the closure, as shown in the Interfaces block. The closure is a value, not a method, and `seed` is `tea.Msg` (an interface holding the `statusMsg` value). |
| The `TestStart_ReachesEventLoop` test is flaky on slow CI (e.g., >2s to start) | Medium | Low | The 2-second timeout is generous. If it still flakes, bump to 5 seconds — liveness tests are not the place to optimize. The existing `TestStart_AttachNotFound` uses 100ms for a similar reason; a 2s budget is a safe margin above that. |
| A future change to Bubble Tea's `Init` semantics (e.g., `Init` becomes asynchronous) breaks the seed path | Low | Low | The seed goes through the same `tea.Cmd` mechanism as every other command in the codebase. If Bubble Tea changes how `Init` works, every command in the program breaks, not just this one. |
| Other conduits (HTTP, Slack, Telegram) have the same `Send`-before-`Run` pattern | Medium | Low | Out of scope for this plan (the bug is in `x/conduit/tui/`). If they share the pattern, the same fix shape applies (route through `Init` if they have a Bubble Tea model; otherwise audit `Send` calls against program lifecycle). Worth a follow-up audit but not a blocker. |
| The `statusFromStream` helper's "defensive copy" semantics (`TestStatusFromStream` case 3) become load-bearing when the closure yields the `statusMsg` — i.e., the live event flow mutates the same map after the seed is built | Low | Low | The helper already returns a defensive copy (L221-228). The closure yields the same `statusMsg` value, so even if the live flow later mutates the underlying metadata, the seed is unaffected. |
| Workshop's `x/conduit/tui` pin is not bumped, so users continue to see the bug | Medium | Medium | Mention the bump in the PR description and `Validation Criteria`. The bump is a one-line `go get` + `go mod tidy` commit in the workshop repo, not part of this plan. Same shape as the prior plan's release-flow mitigation. |
| Removing the `Send` block from `Start` inadvertently removes a comment that future readers find useful (the explanation of *why* the seed is needed) | Low | Low | The substantive rationale is moved into the `initModel` and `Init` field comments, which are at the new code site. The `statusFromStream` function comment (L210-219) also still explains the why. |

## Validation Criteria

- [ ] `go test -race ./x/conduit/tui/...` passes with zero failures.
- [ ] `go test -race ./...` passes in the `ore` repo.
- [ ] `go vet ./...` is clean.
- [ ] `go build ./...` succeeds from the `ore` module root.
- [ ] New test `TestStart_ReachesEventLoop` in `ore/x/conduit/tui/tui_test.go` passes. It exercises `Start` end-to-end with a cancellable context and asserts the program reaches the event loop.
- [ ] New test `TestInitModel_SeedsStatusFromStream` in `ore/x/conduit/tui/tui_test.go` passes. It asserts `initModel` populates `m.initStatusMsg` from `stream.AllMetadata()`, and that the field is `nil` for a stream with no metadata.
- [ ] New test `TestInit_DispatchesSeedCmd` in `ore/x/conduit/tui/tui_test.go` passes. It asserts `m.Init()` returns a `tea.Cmd` that yields the seed `statusMsg`, and returns `nil` when no seed is present.
- [ ] All existing tests continue to pass: `TestNew`, `TestNew_WithThreadID`, `TestNew_WithName`, `TestTUI_ImplementsAudioNotifier`, `TestTUI_InitModel_ResumesThreadWithHistory`, `TestStart_AttachNotFound`, `TestStatusFromStream`, and the `model_test.go` / `view_test.go` suites.
- [ ] The original repro from the captured stack dump is resolved. A manual run of `workshop` (`go run ./cmd/workshop`) starts a TUI session that renders the status bar with `thread_id`, `cwd`, `git_branch`, `tui.pid`, and any `WithDefaultMetadata` keys.
- [ ] No `t.program.Send(...)` call exists in `TUI.Start` before `p.Run()`. A grep of `ore/x/conduit/tui/tui.go` for `program.Send` shows the remaining calls are all in the output-event and user-event goroutines (post-`Run`).
- [ ] The status-bar seed still flows through the existing `statusMsg` handler in `model.go`. No new merge logic is introduced; no direct field writes to `m.status` are added.
- [ ] The `statusFromStream` helper and its `TestStatusFromStream` coverage remain unchanged.
- [ ] **Downstream follow-up (workshop repo, out of plan scope):** once the new `x/conduit/tui` tag is published, bump `github.com/andrewhowdencom/ore/x/conduit/tui` in `workshop/go.mod` and run `go mod tidy`. Workshop's TUI then picks up the fix automatically. (Same shape as the prior plan's release-flow step.)
