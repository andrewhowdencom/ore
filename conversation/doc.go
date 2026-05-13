// Package conversation defines a Store interface and Conversation entity
// for managing persistent, multi-conduit conversation state.
//
// A Conversation holds a stable UUID, a *state.Memory, and timestamps.
// It also provides per-conversation locking (Lock/Unlock) so multiple
// conduits can safely append turns to the same conversation.
//
// Two Store implementations are provided:
//   - MemoryStore: in-memory map, ephemeral
//   - JSONStore: persists conversations as individual .json files
//
// The conversation/ package depends only on artifact/ and state/,
// keeping the dependency graph clean and cycle-free.
package conversation
