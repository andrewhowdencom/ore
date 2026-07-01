// Package ledger defines the State interface and supporting types for ore's
// conversation history model.
//
// State is a mutable interface: Append() mutates in place. Turns() returns
// the active path through the conversation tree (see [Thread.ResolveActivePath])
// as a defensive copy so providers can safely iterate without
// synchronization. The in-memory implementation ([Thread]) is intentionally
// not goroutine-safe; concurrency control is a future middleware concern.
package ledger
