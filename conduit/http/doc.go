// Package http implements an HTTP handler library for the ore framework,
// exposing loop.Step conversation primitives over HTTP with NDJSON streaming
// and SSE ambient channels.
//
// The Handler struct provides methods mountable on any http.ServeMux,
// supporting ephemeral sessions, server-side ReAct loop execution,
// event filtering by kind, and dual transport modes:
//   - NDJSON streaming responses for per-message turns
//   - SSE ambient channels for session-wide real-time events
//
// Callers compose the Handler with a provider and a loop.Step factory,
// then mount the returned ServeMux on an http.Server.
package http
