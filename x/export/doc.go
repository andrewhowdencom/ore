// Package export renders junk.Thread conversation histories into
// human-reviewable formats: plain text, self-contained HTML, and JSON.
//
// All three top-level functions accept an io.Writer and a *junk.Thread,
// iterate over the thread's turns, and emit every artifact in the turn.
// Delta artifacts are never present in persisted threads so no special
// handling is required.
package export
