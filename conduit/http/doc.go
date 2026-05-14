// Package http implements an HTTP handler library for the ore framework,
// exposing session.Session conversation primitives over HTTP with NDJSON
// streaming and SSE ambient channels.
//
// API:
//
//	NewHandler(mgr, opts...)  - create a handler wired to a session.Manager
//	WithUI()                  - enable the built-in web UI (default: disabled)
//	ServeMux()                - returns *http.ServeMux with all routes registered
//
// Routes:
//   POST /sessions                    - create a new session (201)
//   DELETE /sessions/{id}             - close a session
//   POST /sessions/{id}/messages      - send a message; NDJSON response
//   GET  /sessions/{id}/events        - subscribe to events; SSE stream
//   GET  /threads                     - list all threads
//
// Status codes:
//   201  - session created
//   404  - session or thread not found
//   409  - session busy (concurrent request rejected)
//   400  - malformed JSON or unsupported event
//   405  - method not allowed
//   500  - internal error (provider, store, etc.)
//
// Default event kinds for POST /messages responses:
//   text, reasoning, tool_call, tool_result, turn_complete, error
//
// Callers compose the Handler with a session.Manager and mount the
// returned ServeMux on an http.Server. Per-request handlers obtain a
// session.Session handle from the Manager and use it directly for Process
// and Subscribe, while Manager methods remain for metadata and registry
// lifecycle (Store, List, Check, Close).
package http
