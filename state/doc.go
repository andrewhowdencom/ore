// Package state defines the State interface and supporting types for ore's
// conversation history model.
//
// State is a mutable interface: Append() mutates in place. Turns() returns a
// defensive copy of the internal slice so providers can safely iterate without
// synchronization. The in-memory implementation (Buffer) is intentionally not
// goroutine-safe; concurrency control is a future middleware concern.
package state
