// Package thread defines a Store interface and Thread entity
// for managing persistent, multi-conduit thread state.
//
// A Thread holds a stable UUID, a *state.Buffer, and timestamps.
// It also provides per-thread locking (Lock/Unlock) so multiple
// conduits can safely append turns to the same thread. Lock is
// non-blocking: it returns false if the thread is already busy.
//
// Store is the persistence abstraction with five methods:
//   - Create: generate a new Thread with a random UUID
//   - Get: retrieve a Thread by ID
//   - Save: persist a Thread and update its UpdatedAt timestamp
//   - Delete: remove a Thread by ID
//   - List: return all stored Threads
//
// Two Store implementations are provided:
//   - MemoryStore: in-memory map, ephemeral
//   - JSONStore: persists threads as individual .json files
//
// Serialization enforces that delta artifacts (streaming fragments such
// as TextDelta, ReasoningDelta, and ToolCallDelta) are never persisted.
// Attempting to serialize a Thread that contains delta artifacts
// returns an error.
//
// The thread/ package depends only on artifact/ and state/,
// keeping the dependency graph clean and cycle-free.
package thread
