// Package thread defines a Store interface and Thread entity
// for managing persistent, multi-conduit thread state.
//
// A Thread holds a stable UUID, a *state.Buffer, timestamps, and a
// Metadata map for conduit-specific key-value pairs (e.g., external system
// identifiers). Metadata is always initialized as a non-nil empty map when a
// Thread is created via a Store, so conduits can write immediately without a
// nil check. The GetMetadata and SetMetadata methods provide thread-safe
// access to Metadata under a separate read-write lock, independent of the
// per-thread Lock/Unlock used for turn appending.
//
// Store is the persistence abstraction with six methods:
//   - Create: generate a new Thread with a random UUID
//   - Get: retrieve a Thread by ID
//   - GetBy: retrieve the first Thread that matches a metadata key-value pair
//     (linear scan; O(n) time, returns the thread and true on success,
//     nil and false if no thread matches)
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
