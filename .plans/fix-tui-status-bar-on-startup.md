# Plan: Fix TUI status bar empty on session start

## Objective

Fix the regression introduced by #375 (commits `2bd6ba2`, `cd03b29`) where the
TUI status bar is empty on the first rendered frame of a new or resumed
session. The metadata seeded by `Manager.applyDefaultMetadata` — `thread_id`,
`cwd`, `git_branch`, `tui.pid`, and any `WithDefaultMetadata` keys — is
correctly written to the thread, but the `PropertiesEvent`s emitted by
`Stream.SetMetadata` fire *before* the TUI subscribes to the stream's
`FanOut`, so they are delivered to no one. The TUI must bootstrap its status
view directly from the stream, exactly as it already bootstraps historical
turns via `stream.Turns()` (the fix from #328 / commit `f1fdd9c`).

## Context

### Repository topology

The fix spans two packages, both already in the worktree:

- **`session/`** — `Manager`, `Stream`, and `Thread` types. `Thread.Metadata`
  is the persistent key-value map. `Stream.SetMetadata` (line 365 of
  `session/stream.go`) writes to `thread.Metadata` and emits a
  `loop.PropertiesEvent` for each key. `Manager.applyDefaultMetadata`
  (`session/manager.go:183-190`) iterates over the configured default
  metadata and calls `stream.SetMetadata` for each key on both
  `Manager.Create()` (line 127) and `Manager.Attach()` (line 178).
- **`x/conduit/tui/`** — Bubble Tea TUI. `Start` in `tui.go:212-280` calls
  `mgr.Create()` or `mgr.Attach()`, then runs `initModel` (which calls
  `m.loadHistory(stream.Turns())`), creates the program, and finally calls
  `stream.Subscribe(...)`. `statusMsg` in `model.go` is the Bubble Tea
  message that merges into `m.status`; the handler is at
  `x/conduit/tui/model.go` under `case statusMsg:` and is already well
  exercised by `TestModel_Update_Status_*` tests in `model_test.go`.

### Key observations

1. **`Stream.Turns()` is the existing parallel.** It exposes the thread's
   turn history (defensive copy, `session/stream.go:345-353`) and is the
   mechanism `m.loadHistory` consumes. The fix follows the same shape for
   metadata.

2. **`Stream.GetMetadata(key)` exists but returns one value at a time.**
   A bulk accessor is needed so the TUI can seed its entire status map in
   one message without round-tripping per key.

3. **`statusMsg` already does the merge correctly.** The handler initializes
   `m.status` if nil, merges key-by-key, calls `m.recalcLayout()` and
   `m.syncViewport()`. We do not need to duplicate this logic.

4. **`stream.Subscribe` is documented as live-only** (the comment in
   `session/stream.go:271-280` states this explicitly: "Subscribe is
   live-only: it delivers events from the point of subscription onward and
   does not replay historical events. Conduits that need historical state
   should fetch it via Turns() before subscribing, or load it via
   LoadTurns() after external mutations."). The fix respects that
   contract — it does *not* re-introduce event replay.

5. **The `startSinkForwarding` call in `Manager.Create`/`Attach` happens
   *before* `applyDefaultMetadata`** (lines 122-128, 173-178 in
   `session/manager.go`). That means by the time `Start` calls
   `stream.Subscribe`, all default-metadata `PropertiesEvent`s have already
   been emitted and are gone.

6. **Project conventions** (`AGENTS.md`): root module stays stdlib-only, so
   any test deps land in the sub-module; package deps are acyclic;
   aggressive refactoring is preferred over backwards compatibility;
   table-driven tests; `go test -race ./...` is the bar.

## Architectural Blueprint

Two-part change with a clear dependency, mirroring the shape of the
`Stream.Turns()` + `m.loadHistory` fix from #328:

1. **Expose a bulk metadata accessor on `Stream`.** Add
   `func (s *Stream) AllMetadata() map[string]string` to `session/stream.go`
   returning a defensive copy of `s.thread.Metadata`. The stream mutex is
   held for the duration of the copy. A new table-driven test in
   `session/stream_test.go` (`TestStream_AllMetadata`) asserts: empty map
   on a fresh stream, the seeded keys after `applyDefaultMetadata`-style
   writes, and that mutating the returned map does not affect the thread.

2. **Bootstrap the TUI status from the stream at `Start`.** In
   `x/conduit/tui/tui.go`, immediately after `t.program = p` and before the
   `stream.Subscribe(...)` call, send one
   `t.program.Send(statusMsg{status: stream.AllMetadata()})` if the map is
   non-empty. The existing `statusMsg` handler does the merge. The
   `Subscribe` filter list stays exactly as-is — no replay is introduced.

### Why `p.Send(statusMsg{...})` and not a new `loadStatus` model method?

A direct mirror of `m.loadHistory` would extract a new method
`m.loadStatus(map[string]string)` that performs the same merge as the
`statusMsg` handler. **Rejected** because:

- The merge is two lines; duplicating it in a new method creates two paths
  that must stay in sync forever.
- Routing the seed through `p.Send` keeps a single, well-tested path for
  status updates (the `statusMsg` handler) and means a future change to
  status merge semantics is one diff, not two.
- The `loadHistory` helper exists because historical turn rendering needs
  per-artifact `renderArtifact` calls and timestamps — a transformation
  richer than the `statusMsg` handler can express. Status seeding is not in
  that category.

The bootstrap is gated on `len(meta) > 0` so an empty metadata map does
not produce a redundant `statusMsg` (which would re-run `recalcLayout` and
`syncViewport` for no benefit).

## Requirements

1. A new TUI session must show all `WithDefaultMetadata` keys in the status
   bar on the first rendered frame, before any user interaction. [explicit,
   from issue acceptance criteria]
2. Resuming a thread via `--thread <uuid>` must show the same keys
   immediately on resume. [explicit, from issue acceptance criteria]
3. The `Subscribe` contract must remain live-only; no event replay is
   reintroduced. [explicit, from issue acceptance criteria]
4. `Stream.AllMetadata()` must return a defensive copy. Mutating the
   returned map must not mutate the underlying `Thread.Metadata`.
   [inferred, mirrors `state.Buffer.Turns()` and `Stream.Turns()` contracts]
5. `Stream.AllMetadata()` must be safe to call concurrently with
   `Stream.SetMetadata`. [inferred, mirrors `Stream.GetMetadata` locking]
6. All existing tests in `session/...` and `x/conduit/tui/...` must
   continue to pass. [inferred]
7. The fix is scoped to `ore`; the workshop repo's `x/conduit/tui` pin
   will be bumped as a separate downstream commit once the fix is
   released. [inferred from the user's "this should be fixed in ore"
   framing]

## Task Breakdown

### Task 1: Add `Stream.AllMetadata()` accessor with unit test

- **Goal**: Expose the thread's full metadata map as a read-only, defensive
  copy, mirroring the `Stream.Turns()` pattern.
- **Dependencies**: None.
- **Files Affected**:
  - `session/stream.go` — add `AllMetadata()` method.
  - `session/stream_test.go` — add `TestStream_AllMetadata`.
- **New Files**: None.
- **Interfaces**:
  ```go
  // AllMetadata returns a defensive copy of the thread's metadata map.
  // Mutating the returned map does not affect the underlying thread.
  // Returns an empty (non-nil) map if the thread has no metadata.
  func (s *Stream) AllMetadata() map[string]string
  ```
- **Validation**:
  - `go test -race ./session/...` passes.
  - `TestStream_AllMetadata` covers (table-driven):
    1. Fresh stream returns a non-nil empty map.
    2. After `stream.SetMetadata("thread_id", "abc")` and a second key,
       `AllMetadata()` returns both keys with correct values.
    3. Mutating the returned map (`m["x"] = "y"`, `delete(m, "thread_id")`)
       does not affect the stream's underlying metadata (asserted via a
       second `AllMetadata()` call).
    4. The returned map is a fresh reference (`assert.NotSame` /
       `reflect.ValueOf(...).Pointer()` comparison), not the same map the
       thread holds.
- **Details**:
  - Place the method immediately after `GetMetadata` in
    `session/stream.go` (around line 357) to group all metadata accessors.
  - Implementation: acquire `s.mu`, return a fresh `map[string]string`
    populated by ranging over `s.thread.Metadata`. Do not delegate to
    `thread.GetMetadata` (which takes a key). The mutex covers the read of
    the map; the copy is built under the lock so a concurrent
    `SetMetadata` (which also takes the same mutex) cannot race.
  - The test must not call `stream.SetMetadata` after a subscriber is
    attached if it does not want to observe the resulting `PropertiesEvent`
    — but in practice the test never subscribes, so `SetMetadata`'s side
    effect is invisible. This is fine; the contract is "defensive copy on
    return," which the test exercises directly.

### Task 2: Bootstrap TUI status from stream metadata in `Start`

- **Goal**: Send a single `statusMsg` seeded from `stream.AllMetadata()`
  before the live-event goroutine starts, so the first rendered frame
  reflects the stream's current metadata.
- **Dependencies**: Task 1 (`AllMetadata` must exist).
- **Files Affected**:
  - `x/conduit/tui/tui.go` — add bootstrap call in `Start`.
  - `x/conduit/tui/tui_test.go` — add regression test
    `TestStart_BootstrapsStatusFromStream`.
- **New Files**: None.
- **Interfaces**: No new exported types. The change is internal to
  `TUI.Start` and a small package-private helper.
- **Validation**:
  - `go test -race ./x/conduit/tui/...` passes.
  - `TestStart_BootstrapsStatusFromStream` (and a table-driven sibling
    `TestStart_BootstrapsStatus_EmptyMetadata`) cover:
    1. A stream with default metadata (`thread_id`, `cwd`, `git_branch`,
       `tui.pid`, `workshop.role`) — after `Start` runs briefly, the
       status-bar render must contain every key. The test inspects this by
       running `Start` in a goroutine with a 200ms context, then asserts
       on a side-channel: a `tea.Program` filter captures the messages
       sent to the model. The first message must be a `statusMsg` whose
       payload equals `stream.AllMetadata()`.
    2. A stream with empty metadata — the bootstrap is a no-op; the
       model never receives a `statusMsg` from the bootstrap path (it
       may still receive one from a live `PropertiesEvent`, which is
       out of scope for this test).
    3. A resumed thread (via `WithThreadID`) — same as case 1; this is
       the regression case the issue specifically calls out.
  - All existing `TestStart_*` and `TestTUI_InitModel_*` tests must
    continue to pass unchanged. The `TestTUI_InitModel_ResumesThreadWithHistory`
    test exercises `initModel` (which is not touched by this change) and
    must remain green.
- **Details**:
  - In `tui.go`'s `Start`, after the existing line
    `t.program = p` and *before* the line
    `outputCh := stream.Subscribe(...)`, insert:
    ```go
    if meta := stream.AllMetadata(); len(meta) > 0 {
        t.program.Send(statusMsg{status: meta})
    }
    ```
  - Rationale for placement: the bootstrap must fire *before* the
    `Subscribe` call so it is processed by the Bubble Tea event loop in
    the same scheduling slice as the first live event. There is a small
    race between `p.Send` and the Subscribe goroutine that reads from
    `outputCh` and calls `t.program.Send(statusMsg{...})` on every
    `PropertiesEvent`. The bootstrap is harmless under any interleaving —
    it can only *add* keys; the `statusMsg` handler is a merge, not a
    replace.
  - Test strategy: use Bubble Tea v2's `WithFilter` option to capture
    messages as they reach the model. The cleanest path is to refactor
    `Start` to expose the program construction so the test can pass
    `tea.WithFilter(...)` — or, more pragmatically, exercise the
    bootstrap via a package-private helper that the test can call
    directly. The recommended shape:
    ```go
    // statusFromStream returns the statusMsg that should be sent to the
    // program on Start, seeded from the stream's current metadata.
    // Returns nil if there is no metadata to seed.
    func statusFromStream(stream *session.Stream) tea.Msg {
        if meta := stream.AllMetadata(); len(meta) > 0 {
            return statusMsg{status: meta}
        }
        return nil
    }

    // In Start, immediately after `t.program = p`:
    if msg := statusFromStream(stream); msg != nil {
        t.program.Send(msg)
    }
    ```
    The test then asserts the contract of `statusFromStream` directly
    (input stream → output message), and a small integration test
    running `Start` with `tea.WithoutRenderer`, `tea.WithoutSignals`,
    and a 200ms context verifies the message is delivered. This split
    keeps the test simple and fast.
  - No new model method (`loadStatus`) is added. The merge logic stays
    in the existing `statusMsg` handler.
  - No new `WithOption` on the constructor is required.

## Dependency Graph

- Task 1 → Task 2 (Task 2 calls `stream.AllMetadata()`, introduced in
  Task 1)

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| `AllMetadata` returns a shallow copy that aliases an internal map (e.g., if implemented as `s.thread.Metadata` with no copy) and a concurrent `SetMetadata` mutates the returned map | High | Low | Task 1's test case 3 (mutate returned map, re-read, assert unchanged) catches this directly. Implementation must build a fresh map under `s.mu`. |
| Bootstrap `statusMsg` races with a live `PropertiesEvent` from the `Subscribe` goroutine, causing the bootstrap message to be processed *after* a live event for the same key | Low | Low | The `statusMsg` handler is a merge, not a replace. Whichever message arrives last wins, and both messages carry the same value for the default-metadata keys (the live event is emitted by the *same* `SetMetadata` call whose value the bootstrap reads). The end state is identical. |
| Existing TUI tests break because the bootstrap adds a message to the model that they don't expect | Low | Low | The existing `TestTUI_InitModel_ResumesThreadWithHistory` test calls `initModel` directly and never calls `Start`, so the bootstrap (which is in `Start`) is not exercised. `TestStart_AttachNotFound` cancels before any meaningful work. Run `go test -race ./x/conduit/tui/...` to confirm. |
| Other conduits (HTTP, Slack, Telegram) that read status from `PropertiesEvent` are similarly affected but are out of scope for this fix | Low | Medium | The fix is specific to the TUI per the issue. Other conduits can adopt the same pattern (read state, bootstrap on `Start`) as a follow-up. Document this in the PR description but do not expand the task scope. |
| The `statusFromStream` helper adds an exported-for-test symbol that survives into the package's public API | Low | Low | Keep the helper unexported (lowercase `statusFromStream`). It is reachable from `tui_test.go` because the test is in the same package. |
| The downstream `workshop` repo's `x/conduit/tui` version pin is not bumped after the fix lands, so users on `v0.11.1` continue to see the bug | Medium | Medium | Mention the bump in the PR description and in `Validation Criteria` below. The bump is a one-line `go get` + `go mod tidy` commit in the workshop repo, not part of this plan. |
| `applyDefaultMetadata` calls `SetMetadata` for every key, so on a session with N defaults the bootstrap is N times cheaper to deliver than N separate `PropertiesEvent`s would be in a future replay-based world | Low | Low | Not a regression — this is the new (post-#375) contract. Mentioning for completeness. |

## Validation Criteria

- [ ] `go test -race ./session/... ./x/conduit/tui/...` passes with zero
      failures.
- [ ] `go test -race ./...` passes with zero failures in the `ore` repo.
- [ ] New test `TestStream_AllMetadata` in `session/stream_test.go`
      passes, covering empty, populated, and mutation-isolation cases.
- [ ] New test `TestStart_BootstrapsStatusFromStream` in
      `x/conduit/tui/tui_test.go` passes, covering new sessions, resumed
      sessions, and empty-metadata no-ops.
- [ ] The reproduction steps from issue #429 produce a populated status
      bar:
      1. Start a new TUI session.
      2. Observe the first rendered frame: the status bar shows
         `thread_id`, `cwd`, `git_branch`, `tui.pid`, and any
         `WithDefaultMetadata` keys (e.g., `workshop.role`).
      3. Resume an existing thread via `--thread <uuid>`.
      4. Observe: the same keys are visible on the first rendered frame
         after resume.
- [ ] `stream.Subscribe` remains live-only. No code path in the
      `session/` package replays historical events to subscribers. The
      `Subscribe` documentation comment is unchanged.
- [ ] The `x/conduit/tui` module's `go.mod` version is bumped and tagged
      (this happens in a follow-up release commit, not in this plan's
      tasks).
- [ ] **Downstream follow-up (workshop repo, out of plan scope):** once
      the new `x/conduit/tui` tag is published, bump
      `github.com/andrewhowdencom/ore/x/conduit/tui` in
      `workshop/go.mod` and run `go mod tidy`. Workshop's TUI will then
      pick up the fix automatically.
