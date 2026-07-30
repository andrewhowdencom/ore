// Package http implements an HTTP handler library for the ore framework.
// The conduit translates external HTTP requests into session events and
// renders session output as Server-Sent Events (SSE).
//
// # Architecture
//
// The conduit is a dumb pipe: it owns no provider, agent, factory, queue,
// or persistent state. It depends on an application-supplied Backend
// (see backend.go) for session lifecycle and event submission, and on
// session.Subscribe for output observation.
//
// # Wire
//
// All endpoints return JSON or text/event-stream. Errors are signalled
// by HTTP status codes; the body is JSON for 4xx/5xx responses.
//
//	POST   /sessions                       Create a new (or attach to a thread)
//	                                       session.
//	GET    /sessions/{id}                  Return metadata for the session.
//	DELETE /sessions/{id}                  Close the session (the thread is
//	                                       preserved).
//	GET    /threads                         Paginated list of persisted threads.
//	POST   /sessions/{id}/events            Submit a session.Event (e.g. user
//	                                       message, interrupt). Returns 202 on
//	                                       admission.
//	GET    /sessions/{id}/events?kinds=...  Server-Sent Events stream of the
//	                                       session's authoritative output
//	                                       stream.
//
// # Lifecycle
//
// The handler is constructed with a Backend and started via
// Start(ctx). On context cancellation, the HTTP server is shut down
// gracefully and Start returns nil.
package http
