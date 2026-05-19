# Plan: Add thread metadata and lookup for ambient conduit thread mapping

## Objective

Add a `Metadata map[string]string` field to `thread.Thread` and a `GetBy(key, value string) (*Thread, bool)` method to `thread.Store`, enabling ambient conduits (Slack, Discord, Telegram, etc.) to map external system identifiers to ore thread IDs. This allows conduits to resume existing threads by external key rather than only by framework-generated UUID.

## Context

The `thread` package (located at `thread/`) defines the `Store` interface and `Thread` entity for managing persistent, multi-conduit thread state. The package is imported by `session/`, `x/conduit/http/`, `x/conduit/tui/`, and `examples/`.

**Current `Thread` struct** (`thread/store.go`):
```go
type Thread struct {
    ID        string
    State     *state.Buffer
    CreatedAt time.Time
    UpdatedAt time.Time
    mu        sync.Mutex
    busy      bool
}
```

**Current `Store` interface** (`thread/store.go`):
```go
type Store interface {
    Create() (*Thread, error)
    Get(id string) (*Thread, bool)
    Save(thread *Thread) error
    Delete(id string) bool
    List() ([]*Thread, error)
}
```

**Implementations**:
- `MemoryStore` (`thread/memory.go`): in-memory map, ephemeral
- `JSONStore` (`thread/json.go`): persists threads as individual `.json` files with an in-memory cache

**Custom JSON serialization** (`thread/store.go`): `Thread` has `MarshalJSON`/`UnmarshalJSON` methods that use a local `jsonThread` struct wrapping `ID`, `CreatedAt`, `UpdatedAt`, and `Turns`.

**Mock `Store` implementations outside `thread/` package**:
- `errStore` in `session/manager_test.go`
- `errStore`, `saveErrStore`, `listErrStore` in `x/conduit/http/handler_test.go`

All mock implementations explicitly define methods for the `Store` interface. Adding a new method to `Store` requires updating every mock, or the codebase will fail to compile when those types are assigned to `thread.Store`.

## Architectural Blueprint

The change is purely additive: extend the data model with a `Metadata` map and the interface contract with a `GetBy` lookup method. No existing behavior is altered.

**Design decisions** (as specified in the issue):
- **Uniqueness enforcement**: deferred to a future data-store layer. For now, `GetBy` returns the first match.
- **Return type**: `(*Thread, bool)` mirroring `Get(id string)`.
- **Key/value types**: `string, string` — flexible for any conduit.
- **Lookup strategy**: linear scan over in-memory structures (map for `MemoryStore`, cache for `JSONStore`).
- **JSONStore cache-only search**: `GetBy` scans only the in-memory cache. All threads created through `JSONStore` are immediately cached, and `NewJSONStore` loads all existing `.json` files on initialization.

## Requirements

1. Add `Metadata map[string]string` field to `Thread` struct. [explicit]
2. Initialize `Metadata` as a non-nil map in `MemoryStore.Create()` and `JSONStore.Create()`. [inferred]
3. Add `GetBy(key, value string) (*Thread, bool)` to `Store` interface. [explicit]
4. Implement `GetBy` in `MemoryStore` with linear scan over `s.threads`. [explicit]
5. Implement `GetBy` in `JSONStore` with linear scan over `s.cache`. [explicit]
6. Update `Thread.MarshalJSON` and `Thread.UnmarshalJSON` to include `Metadata`. [explicit]
7. Update all mock `Store` implementations outside `thread/` to include `GetBy`. [inferred]
8. Add tests for `GetBy` in `memory_test.go` and `json_test.go`. [explicit]
9. Add tests for metadata JSON round-trip in `integration_test.go`. [inferred]
10. Update `thread/doc.go` to document `Metadata` and `GetBy`. [inferred]
11. Run `go test -race ./...` to verify thread safety. [explicit]

## Task Breakdown

### Task 1: Add Metadata field to Thread struct and JSON serialization
- **Goal**: Extend `Thread` with a `Metadata` field and update JSON serialization to persist it.
- **Dependencies**: None.
- **Files Affected**:
  - `thread/store.go` — add `Metadata` to `Thread` struct, add `Metadata` to `jsonThread` wrapper in `MarshalJSON`/`UnmarshalJSON`
  - `thread/memory.go` — initialize `Metadata: make(map[string]string)` in `Create()`
  - `thread/json.go` — initialize `Metadata: make(map[string]string)` in `Create()`
  - `thread/integration_test.go` — update `TestThread_MarshalJSON` to populate and assert `Metadata`
- **New Files**: None.
- **Interfaces**:
  - `Thread` struct gains `Metadata map[string]string` field
  - `jsonThread` internal struct gains `Metadata map[string]string \`json:"metadata,omitempty"\``
  - `UnmarshalJSON` must initialize `Metadata` to `make(map[string]string)` when JSON field is absent or null, to prevent nil-map panics on subsequent writes
- **Validation**:
  - `go test ./thread/...` passes
  - `TestThread_MarshalJSON` asserts metadata round-trips correctly
- **Details**:
  - In `MarshalJSON`, add `Metadata: c.Metadata` to the `jsonThread` wrapper.
  - In `UnmarshalJSON`, after unmarshaling `jc`, set `c.Metadata = jc.Metadata` and guard against nil by initializing an empty map if nil.
  - Initialize `Metadata` in both `MemoryStore.Create()` and `JSONStore.Create()` so conduits can immediately assign key-value pairs without panics.

### Task 2: Add GetBy to Store interface and implement in both stores
- **Goal**: Add `GetBy(key, value string) (*Thread, bool)` to `Store` and implement in `MemoryStore`, `JSONStore`, and all mock implementations.
- **Dependencies**: Task 1 (Metadata field must exist before `GetBy` can search it meaningfully).
- **Files Affected**:
  - `thread/store.go` — add `GetBy(key, value string) (*Thread, bool)` to `Store` interface
  - `thread/memory.go` — implement `GetBy` with `RLock`, linear scan over `s.threads`
  - `thread/json.go` — implement `GetBy` with `RLock`, linear scan over `s.cache`
  - `session/manager_test.go` — add `GetBy` to `errStore`
  - `x/conduit/http/handler_test.go` — add `GetBy` to `errStore`, `saveErrStore`, and `listErrStore`
- **New Files**: None.
- **Interfaces**:
  - `Store` interface: `GetBy(key, value string) (*Thread, bool)`
  - `MemoryStore.GetBy(key, value string) (*Thread, bool)`
  - `JSONStore.GetBy(key, value string) (*Thread, bool)`
- **Validation**:
  - `go test ./...` compiles and passes (all mock implementations must compile)
- **Details**:
  - `MemoryStore.GetBy`: acquire `RLock`, iterate `s.threads`, check `thread.Metadata[key] == value`, return first match. If no match, return `nil, false`.
  - `JSONStore.GetBy`: acquire `RLock`, iterate `s.cache`, same matching logic.
  - `errStore.GetBy` (session): return `thread.NewMemoryStore().GetBy(key, value)` or `nil, false` depending on the mock's purpose.
  - `errStore.GetBy` (http): return `nil, false`.
  - `saveErrStore.GetBy`: delegate to `s.inner.GetBy(key, value)`.
  - `listErrStore.GetBy`: return `nil, false`.
  - This task is atomic: the interface change and all implementations (including mocks) must land in a single commit, or the build breaks.

### Task 3: Add comprehensive tests for GetBy and metadata persistence
- **Goal**: Add tests for `GetBy` behavior and metadata round-trip across stores.
- **Dependencies**: Task 2.
- **Files Affected**:
  - `thread/memory_test.go` — add `TestMemoryStore_GetBy` and `TestMemoryStore_GetBy_NotFound`
  - `thread/json_test.go` — add `TestJSONStore_GetBy` and `TestJSONStore_GetBy_NotFound`
  - `thread/integration_test.go` — update `TestJSONStore_CrossConduitContinuity` to set and verify metadata
- **New Files**: None.
- **Validation**:
  - `go test ./thread/...` passes
  - New tests cover: exact match, not found, multiple threads with different metadata values
- **Details**:
  - `TestMemoryStore_GetBy`: create two threads, set `Metadata["channel_id"] = "123"` on one, assert `GetBy("channel_id", "123")` returns the correct thread, assert `GetBy("channel_id", "999")` returns `nil, false`.
  - `TestJSONStore_GetBy`: same pattern as MemoryStore, but save and reload via a second `JSONStore` instance to verify persistence.
  - `TestJSONStore_CrossConduitContinuity`: add metadata to the thread before saving, assert it survives process restart.

### Task 4: Update documentation and run race tests
- **Goal**: Update package documentation and verify thread safety under the race detector.
- **Dependencies**: Task 3.
- **Files Affected**:
  - `thread/doc.go` — add `Metadata` and `GetBy` to the package-level documentation
- **New Files**: None.
- **Validation**:
  - `go test -race ./...` passes
- **Details**:
  - Update `doc.go` to mention: `Thread` now carries a `Metadata map[string]string` for conduit-specific key-value pairs; `Store` provides `GetBy(key, value)` for lookup by metadata.
  - Run `go test -race ./...` and fix any race conditions introduced by the new code (e.g., ensure `GetBy` holds the correct lock while reading `Metadata`).

## Dependency Graph

- Task 1 → Task 2 (GetBy searches Metadata; mocks need the interface extended)
- Task 2 → Task 3 (tests depend on GetBy implementation)
- Task 3 → Task 4 (race tests run after all behavior is verified)

## Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| Adding `GetBy` to `Store` breaks compilation of mock types | High | High | Task 2 must update all mock implementations atomically in the same commit. |
| `Metadata` JSON field absent in existing persisted threads causes nil map panic on write | High | Medium | Task 1 initializes `Metadata` to empty map in `UnmarshalJSON` when JSON field is missing or null. |
| Race detector flags concurrent access to `Metadata` map | Medium | Medium | Task 4 runs `go test -race ./...`; if races are found, add copy-on-return or document that callers must hold the thread lock. |
| Multiple threads with same metadata key-value pair; `GetBy` returns arbitrary first match | Low | Low | Accepted per issue: uniqueness enforcement is deferred to future data-store layer. Document this behavior. |

## Validation Criteria

- [ ] `go test ./...` passes after each task.
- [ ] `go test -race ./...` passes after Task 4.
- [ ] `Thread.MarshalJSON` / `UnmarshalJSON` round-trip preserves `Metadata` values.
- [ ] `MemoryStore.GetBy` returns the correct thread by metadata key-value pair.
- [ ] `JSONStore.GetBy` returns the correct thread by metadata key-value pair after process restart.
- [ ] All mock `Store` implementations compile when assigned to `thread.Store`.
- [ ] `thread/doc.go` accurately documents `Metadata` and `GetBy`.
