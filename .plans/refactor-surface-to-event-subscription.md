# Plan: Refactor Surface Egress to Event Subscription Model

## Objective

Refactor the Surface interface from imperative egress methods (`RenderDelta`, `RenderTurn`) to an event-subscription model where the framework provides fan-out from `loop.OutputEvent`. Surfaces initialize subscriptions for the event types they handle, and the framework owns the routing plumbing so every surface author does not reimplement the type-switch goroutine. The design must be extensible to additional event types in the future without breaking the Surface contract or the fan-out API. `Events()`, `Run()`, and `SetStatus()` remain unchanged.

## Context

The current codebase has a structural mismatch between the loop's unified event stream and the Surface's imperative method contract.

**Current loop output:** `loop.Step` emits `loop.OutputEvent` (a single interface with `Kind() string`) via a channel. Two concrete types exist: `DeltaEvent` and `TurnCompleteEvent`.

**Current Surface contract:** `surface.Surface` declares three separate egress methods:
- `RenderDelta(ctx, delta artifact.Artifact) error`
- `RenderTurn(ctx, turn state.Turn) error`
- `SetStatus(ctx, status string) error`

**Current application wiring:** In `examples/tui-chat/main.go`, a goroutine subscribes to `loop.OutputEvent`, type-switches, and manually calls `s.RenderDelta()` or `s.RenderTurn()`. This pattern must be replicated for every surface implementation.

**Key files:**
- `surface/surface.go` — Surface interface definition
- `surface/tui/tui.go` — TUI surface implementing RenderDelta/RenderTurn/SetStatus
- `surface/tui/model.go` — Bubble Tea model handling `deltaMsg`, `turnMsg`, `statusMsg` (internal, unchanged)
- `examples/tui-chat/main.go` — Manual event routing goroutine
- `loop/loop.go` — Step with `OutputEvent`, `DeltaEvent`, `TurnCompleteEvent`

**Design tension:** The loop already solved event unification internally, but the Surface contract forces every application to reimplement routing. With a large number of planned surfaces, this is unsustainable.

## Architectural Blueprint

### Selected Architecture

A **FanOut** type is added to the `loop` package (co-located with `OutputEvent` which it distributes). It reads from a `<-chan OutputEvent` source and provides `Subscribe(kind string) <-chan OutputEvent` for filtered subscriptions by event kind. Surfaces create subscriptions during initialization and handle events in their own background goroutines.

**New Surface interface:**
```go
type Surface interface {
    Events() <-chan Event
    SetStatus(ctx context.Context, status string) error
    Run() error
}
```

`RenderDelta` and `RenderTurn` are removed. `SetStatus` remains as an explicit method (deferred to future work).

**FanOut API (in `loop`):**
```go
type FanOut struct{ /* internal */ }

// NewFanOut creates a FanOut that reads from src until src is closed
// or the FanOut is closed.
func NewFanOut(src <-chan OutputEvent) *FanOut

// Subscribe returns a receive-only channel that receives all OutputEvents
// whose Kind() matches the given kind. The channel is closed when the
// FanOut is closed. The caller must read from the channel or provide
// sufficient buffer capacity to avoid blocking the FanOut.
func (f *FanOut) Subscribe(kind string) <-chan OutputEvent

// Close stops the FanOut and closes all subscriber channels.
func (f *FanOut) Close() error
```

**TUI surface refactored:**
- `tui.New(fanOut *loop.FanOut) *TUI` — constructor accepts FanOut
- Creates subscriptions for `"delta"` and `"turn_complete"`
- Background goroutines read from subscription channels and send `deltaMsg`/`turnMsg` into the Bubble Tea program
- `RenderDelta`/`RenderTurn` methods removed; `SetStatus` stays

**Application wiring (tui-chat example):**
- Create `outputCh := make(chan loop.OutputEvent, 100)`
- Create `fanOut := loop.NewFanOut(outputCh)`
- Pass `fanOut` to `tui.New(fanOut)`
- Remove manual routing goroutine

### Tree-of-Thought Deliberation

**Alternative A: Surface gets `HandleEvent(event)` method, framework calls it directly.**
- *Why rejected:* Surfaces would still receive all events and need to type-switch internally. This doesn't solve the "every surface reimplements routing" problem; it just moves the type-switch from the application into the surface.

**Alternative B: Separate top-level event bus package (e.g., `event/` or `bus/`).**
- *Why rejected:* Adds a new top-level package for a single type, increasing conceptual surface area. Creates a three-way dependency (bus imports loop for OutputEvent, surface imports bus, application imports both). The FanOut is tightly coupled to `loop.OutputEvent`; co-location is clearer.

**Alternative C: FanOut lives in `surface/` package.**
- *Why rejected:* `surface/` is the UI contract package. Adding channel plumbing there leaks implementation detail into the interface package. `loop/` already owns `OutputEvent`; it is the natural home for distributing that event stream.

**Selected path:** FanOut in `loop/`, Surface interface simplified, surfaces subscribe to FanOut. Minimal new code, no dependency cycles (`surface/` → `loop/` → `provider/` → `state/` → `artifact/`, no reverse edges), and extensible.

### Extensibility

Future event types (e.g., `ToolCallEvent`, `ToolResultEvent`, `StatusEvent`) implement `loop.OutputEvent` with their own `Kind()` string. Surfaces subscribe to them using the same `Subscribe(kind)` API. Neither `FanOut` nor `Surface` interface requires changes.

## Requirements

1. `Surface` interface no longer declares `RenderDelta` or `RenderTurn`.
2. `loop` package provides a `FanOut` type that distributes `OutputEvent` to filtered subscribers by event kind.
3. TUI surface (`surface/tui/`) refactored to use `FanOut` subscriptions instead of implementing `RenderDelta`/`RenderTurn`.
4. `examples/tui-chat/` updated to use `FanOut` and remove manual event routing goroutine.
5. All existing tests pass; new tests added for `FanOut` behavior.
6. `go test -race ./...` passes with no goroutine leaks.
7. Design is extensible: new event types can be added to `loop.OutputEvent` without changing `FanOut` API or `Surface` interface.

## Task Breakdown

### Task 1: Add FanOut to loop package
- **Goal:** Add a `FanOut` type that reads `loop.OutputEvent` from a source channel and routes to subscribers filtered by event `Kind()`.
- **Dependencies:** None
- **Files Affected:** `loop/loop.go` (reference only, may need no changes), `loop/fanout.go` (new), `loop/fanout_test.go` (new)
- **New Files:** `loop/fanout.go`, `loop/fanout_test.go`
- **Interfaces:**
  ```go
  type FanOut struct{ /* internal */ }
  func NewFanOut(src <-chan OutputEvent) *FanOut
  func (f *FanOut) Subscribe(kind string) <-chan OutputEvent
  func (f *FanOut) Close() error
  ```
- **Validation:** `go test ./loop/...` passes, including new `FanOut` tests. Run with `-race`.
- **Details:** Create `loop/fanout.go`. `NewFanOut` starts a background goroutine that reads from `src` until it is closed or `Close()` is called. `Subscribe` registers a subscriber for a specific event kind; returned channel is closed on `Close()`. Handle context cancellation gracefully. In `loop/fanout_test.go`, add table-driven tests for: single subscriber receiving correct events, multiple subscribers on different kinds, subscriber receiving no events for non-matching kinds, `Close()` closing all subscriber channels, and concurrent access (verified with `go test -race`).

### Task 2: Refactor Surface interface
- **Goal:** Remove `RenderDelta` and `RenderTurn` from the `Surface` interface.
- **Dependencies:** Task 1 (conceptual — the replacement mechanism must exist before the old one is removed, even though compilation is not strictly blocked)
- **Files Affected:** `surface/surface.go`
- **New Files:** None
- **Interfaces:**
  ```go
  type Surface interface {
      Events() <-chan Event
      SetStatus(ctx context.Context, status string) error
      Run() error
  }
  ```
- **Validation:** `go build ./surface/...` passes. Expect downstream compile failures in `surface/tui/` and `examples/tui-chat/` — these are resolved in Task 3 and Task 4.
- **Details:** Edit `surface/surface.go`. Remove `RenderDelta` and `RenderTurn` method declarations. Update package doc comment to describe the new event-driven model: surfaces receive events via subscription rather than imperative method calls. Keep `SetStatus` unchanged. Do not touch `surface/event.go` — ingress events (`UserMessageEvent`, `InterruptEvent`) are unaffected.

### Task 3: Refactor TUI surface to use subscriptions
- **Goal:** Update `surface/tui/` to use `loop.FanOut` subscriptions instead of implementing `RenderDelta`/`RenderTurn`.
- **Dependencies:** Task 1, Task 2
- **Files Affected:** `surface/tui/tui.go`
- **New Files:** None
- **Interfaces:** Constructor changes from `New() *TUI` to `New(fanOut *loop.FanOut) *TUI`.
- **Validation:** `go test ./surface/...` passes, `go build ./...` passes.
- **Details:** In `surface/tui/tui.go`, update `New` to accept `*loop.FanOut`. Create two subscriptions: `fanOut.Subscribe("delta")` and `fanOut.Subscribe("turn_complete")`. Spawn background goroutines that read from each subscription channel and send `deltaMsg`/`turnMsg` into `t.program` (exactly as `RenderDelta`/`RenderTurn` did, just from goroutines instead of methods). Remove `RenderDelta` and `RenderTurn` methods entirely. `SetStatus` stays. Ensure goroutines are cleaned up appropriately when the TUI shuts down (coordinate with `Run()` return or Bubble Tea program lifecycle). Do NOT modify `surface/tui/model.go`, `view.go`, or `model_test.go` — the Bubble Tea message types (`deltaMsg`, `turnMsg`) are unchanged.

### Task 4: Update tui-chat example
- **Goal:** Update `examples/tui-chat/main.go` to use the new `FanOut`-based wiring.
- **Dependencies:** Task 1, Task 2, Task 3
- **Files Affected:** `examples/tui-chat/main.go`
- **New Files:** None
- **Interfaces:** None
- **Validation:** `go build ./examples/tui-chat` passes.
- **Details:** In `examples/tui-chat/main.go`, after creating `outputCh`, create `fanOut := loop.NewFanOut(outputCh)`. Pass `fanOut` to `tui.New(fanOut)`. Remove the manual goroutine that did the `switch e := event.(type)` routing from `outputCh` to `s.RenderDelta()`/`s.RenderTurn()`. The `SetStatus` calls before and after `react.Run()` remain unchanged. Ensure the example compiles cleanly.

### Task 5: Add integration tests and verify race-free
- **Goal:** Ensure the new event-subscription model works end-to-end and is race-free.
- **Dependencies:** Task 1, Task 2, Task 3, Task 4
- **Files Affected:** `loop/fanout_test.go` (extend if needed)
- **New Files:** None (or extend existing test file)
- **Interfaces:** None
- **Validation:** `go test -race ./...` passes with zero failures.
- **Details:** Extend `loop/fanout_test.go` with tests that simulate a full streaming provider emitting `DeltaEvent` and `TurnCompleteEvent` through a `FanOut`, verifying correct routing to subscribers. Run the full test suite with race detection. Verify no goroutine leaks (use `runtime.NumGoroutine` assertions in `FanOut` tests if appropriate). Confirm `surface/tui/model_test.go` passes unchanged — this validates that the internal Bubble Tea model was not affected by the surface refactor.

## Dependency Graph

- Task 1 → Task 2 (FanOut must exist before Surface interface removes old methods)
- Task 1 → Task 3 (TUI needs FanOut)
- Task 2 → Task 3 (TUI needs updated interface)
- Task 3 → Task 4 (example needs refactored TUI)
- Task 4 → Task 5 (integration tests need everything built)

Parallelizable: None — this is a linear refactor where each task depends on the previous one being in a valid state.

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Goroutine leaks in FanOut | High | Medium | Careful lifecycle: `Close()` drains source, closes all subscriber channels, and stops the background goroutine. Use `sync.WaitGroup` or `context.Done` pattern. Verify with goroutine count assertions in tests. |
| Breaking change to Surface interface affects external consumers | High | Low | Only `surface/tui/` and `examples/tui-chat/` in this repo use the methods. No external consumers exist. Blast radius is fully contained. |
| Race conditions in subscription handling | High | Medium | `go test -race ./...` is mandatory in Task 5. Use buffered subscriber channels with reasonable defaults (e.g., 100) to decouple FanOut write speed from surface consumption speed. |
| Bubble Tea message loop conflicts with background goroutines | Medium | Medium | Keep existing `deltaMsg`/`turnMsg` pattern — only the sender changes (from method call to goroutine reading from channel). `model.go` and its tests are untouched. |
| TUI constructor signature change breaks other examples | Medium | Low | Only `examples/tui-chat/` constructs a TUI. No other examples use `surface/tui/`. |
| FanOut subscriber blocking stalls entire stream | High | Medium | Document that subscribers must read from channels promptly. Use buffered channels. Consider a non-blocking send with drop for slow subscribers in a future enhancement, but not required for this plan. |

## Validation Criteria

- [ ] `FanOut` correctly routes `DeltaEvent` to `"delta"` subscribers.
- [ ] `FanOut` correctly routes `TurnCompleteEvent` to `"turn_complete"` subscribers.
- [ ] `FanOut` does not send non-matching events to a subscriber.
- [ ] `FanOut.Close()` closes all subscriber channels and stops the background goroutine.
- [ ] `Surface` interface compiles with only `Events()`, `SetStatus()`, `Run()`.
- [ ] TUI surface compiles and tests pass without `RenderDelta`/`RenderTurn`.
- [ ] `surface/tui/model_test.go` passes unchanged.
- [ ] `examples/tui-chat` compiles successfully.
- [ ] `go test -race ./...` passes with zero failures.
- [ ] No goroutine leaks detected in FanOut tests.
- [ ] Adding a new event type (e.g., a test event with new `Kind()`) works with existing `Subscribe` API without code changes to `FanOut`.
