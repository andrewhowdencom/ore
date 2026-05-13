// Package conversation defines a Store interface and Conversation entity
// for managing persistent, multi-conduit conversation state.
//
// A Conversation holds a stable UUID, a *state.Memory, and timestamps.
// It also provides per-conversation locking (Lock/Unlock) so multiple
// conduits can safely append turns to the same conversation. Lock is
// non-blocking: it returns false if the conversation is already busy.
//
// Store is the persistence abstraction with five methods:
//   - Create: generate a new Conversation with a random UUID
//   - Get: retrieve a Conversation by ID
//   - Save: persist a Conversation and update its UpdatedAt timestamp
//   - Delete: remove a Conversation by ID
//   - List: return all stored Conversations
//
// Two Store implementations are provided:
//   - MemoryStore: in-memory map, ephemeral
//   - JSONStore: persists conversations as individual .json files
//
// Serialization enforces that delta artifacts (streaming fragments such
// as TextDelta, ReasoningDelta, and ToolCallDelta) are never persisted.
// Attempting to serialize a Conversation that contains delta artifacts
// returns an error.
//
// The conversation/ package depends only on artifact/ and state/,
// keeping the dependency graph clean and cycle-free.
package conversation
