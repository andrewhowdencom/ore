---
name: conduit
description: |
  Implements a new ore I/O conduit package under x/conduit/<name>/ using the
  functional-options constructor pattern, exported Descriptor for discovery, and
  blocking Start(ctx) lifecycle. Dumb pipe that translates external system events
  (HTTP, TUI, chat bot, webhook) into ore session events via session.Manager,
  subscribes to broadcast FanOut output streams, and routes text/reasoning/image
  artifacts back to external systems. Compatible with the broadcast
  multi-conduit model. Does NOT handle cognitive orchestration,
  provider invocation, or turn-loop management.
---

# Ore Conduit

## When to Use

This skill is triggered **ONLY** when implementing a **NEW** I/O conduit package
under `x/conduit/<name>/` or modifying an existing conduit implementation.

Do NOT use this skill for:
- Core package work (`artifact/`, `loop/`, `session/`, `provider/`, `state/`, `thread/`, `cognitive/`)
- Modifying ore `AGENTS.md` or repository-level documentation
- Go language or tooling questions (see `go/` skill instead)
- Ore architectural philosophy or package boundary decisions (see `AGENTS.md`)

## What is a Conduit

An ore conduit is a dumb pipe that translates events between an external system
and the ore framework. It is not a "UI" in the narrow sense, nor is it a
cognitive agent. A conduit's only job is ingress (mapping external events into
`session.UserMessageEvent` and pushing them into a stream) and egress
(subscribing to the stream's broadcast output and routing assistant artifacts
back to the external system).

Conduits must never import `cognitive/` packages, invoke `provider.Invoke()`
directly, or manage turn loops. Those are application-level concerns composed
in `examples/` or `cmd/` packages. The conduit library exposes `Session`,
`Step`, and `State` via exported accessors so the application layer can call
`Step.Submit()`, `Step.Turn()`, or run a full `cognitive.ReAct` loop as
needed. See `AGENTS.md` for the full Conduit/Library vs. Application boundary.

## Execution Procedure

Follow these steps in order. Do not skip or reorder.

1. **Create package `x/conduit/<name>/`** with its own `go.mod`. Use
   `replace github.com/andrewhowdencom/ore => ../../..` to link the core module.
2. **Implement `conduit.Conduit`** — a type with exactly one method:
   `Start(ctx context.Context) error`.
3. **Accept `*session.Manager`** via the constructor using the functional options
   pattern:
   ```go
   func New(mgr *session.Manager, opts ...Option) (conduit.Conduit, error)
   ```
   Validate `mgr != nil`; return an error if nil. Functional options can
   override defaults but must not be required.
4. **In `Start()`**: create or attach a session:
   - `stream, err := mgr.Create()` for a new session
   - `stream, err := mgr.Attach(threadID)` to resume an existing thread
   In multi-conduit agents, multiple conduits share the same `*session.Manager`.
   Each conduit calls `mgr.Create()` or `mgr.Attach()` independently.
5. **Subscribe to output events** from the stream:
   ```go
   outputCh := stream.Subscribe("turn_complete")
   ```
   For streaming conduits, subscribe to artifact kinds directly
   (`"text_delta"`, `"reasoning_delta"`, etc.).
   The stream uses a FanOut broadcast model. Multiple conduits can subscribe
   concurrently; each receives all events independently.
6. **Capture your delivery mechanism in the subscriber closure.** The subscriber
   goroutine must close over the external-system client (HTTP writer, Slack
   client, TUI program, etc.). Destination routing is **NOT** carried in
   `EventContext.Provenance`.
   If delivery to the external system fails, log the error (non-fatal) and
   continue. Optionally render a failure message if the transport supports it.
7. **Set up the external input → `stream.Process()` loop.** Map all external
   events to `session.UserMessageEvent{Content: ...}` and call
   `stream.Process(ctx, event)`. For cancellation, use
   `session.InterruptEvent{}`.
   Before calling `stream.Process()`, set `EventContext.Provenance` to the
   conduit's identifier on outbound `UserMessageEvent`. When receiving events,
   check if `Provenance` matches your own identifier and skip processing to
   avoid echo loops.
8. **Export a `Descriptor` variable** at the package level enumerating the
   well-known capabilities this conduit supports:
   ```go
   var Descriptor = conduit.Descriptor{
       Name:        "MyConduit",
       Description: "One-line description",
       Capabilities: []conduit.Capability{
           conduit.CapEventSource,
           conduit.CapRenderTurn,
       },
   }
   ```
9. **Block in `Start()`** until `ctx.Done()` signals shutdown. Return `nil` on
   clean shutdown; return non-nil only on fatal startup or runtime errors.
   Fatal errors (startup failure, unrecoverable connection loss to the external
   system) MUST return non-nil from `Start()`, which triggers agent-level
   shutdown. Non-fatal errors (delivery failure to one recipient, transient
   timeout) MUST be logged and the conduit MUST continue.
10. **Add table-driven tests** with a mock `session.Manager` or
    `httptest.Server`. Verify `Start()` blocks, `Descriptor` is exported, and
    the constructor rejects nil manager.
11. **Run `go test -race ./...`** from the package directory. All tests must
    pass.
12. **Write `README.md`** in the package root (`x/conduit/<name>/README.md`)
    documenting how to compose the conduit. Follow the structure in
    `./README_EXAMPLE.md`. Include: Overview, Capabilities, Composition,
    Configuration, Runtime Semantics, and Error Handling.

> See `./SKELETON.md` for a compilable skeleton, `x/conduit/doc.go` for
> the standard contract, and `./README_EXAMPLE.md` for the composition guide
> template.

## Success Criteria

After implementing a conduit, verify:

- [ ] Package exports `Descriptor` with valid capabilities
- [ ] Constructor accepts `*session.Manager` and validates non-nil
- [ ] `Start(ctx)` blocks until `ctx.Done()`
- [ ] Subscribes to output events before blocking
- [ ] Maps all external inputs to `UserMessageEvent` or `InterruptEvent`
- [ ] Passes `go test -race ./...`
- [ ] Handles provenance echo suppression
- [ ] `README.md` is present with all required sections (see `./README_EXAMPLE.md`)

## Boolean Guards

If any of the following are true, **STOP** and reassess:

- ⚠️ **IF** importing `cognitive/` → STOP. Conduits are dumb pipes. Cognitive
  patterns (ReAct, chain-of-thought) belong in the application layer, not the
  conduit.
- ⚠️ **IF** calling `provider.Invoke()` directly → STOP. Use
  `stream.Process()` which delegates to the session manager and turn processor.
- ⚠️ **IF** managing turn loops, tool execution loops, or ReAct logic → STOP.
  That is the `cognitive/` package's responsibility or the application's
  `TurnProcessor`.
- ⚠️ **IF** putting destination routing metadata (channel ID, thread ID, email
  address) into `EventContext.Provenance` → STOP. Capture the delivery mechanism
  in the subscriber closure. `Provenance` is for source metadata only.
- ⚠️ **IF** the conduit requires mandatory constructor options (not just
  functional options with defaults) → STOP. Conduits should use functional
  options with sensible defaults so they compose easily in hand-written main.go
  files.

## Gotchas

1. **Subscriber backpressure.** `stream.Subscribe()` channels have a fixed buffer
   of 100 events. Slow subscribers silently drop events. Design for idempotency
   or tolerate missing deltas. Do not assume reliable delivery.
2. **Hardcoded event types.** `Stream.Process()` only accepts
   `session.UserMessageEvent` and `session.InterruptEvent`. All external inputs
   must map to one of these. Custom event types require changes to the
   `session` package.
3. **Provenance is source-only.** `EventContext.Provenance` is a `string` for
   echo suppression and audit trails. It is NOT for destination routing metadata
   (channel IDs, thread timestamps, email addresses). Capture the delivery
   mechanism in the subscriber closure.
4. **MarshalArtifact is hardcoded.** `x/conduit/http/types.go` has a fixed switch
   for core artifact kinds. If your conduit defines custom artifact types and
   uses HTTP transport, you must extend the marshal/unmarshal functions or
   handle serialization yourself.
5. **Closed subscription on dead session.** If a session is closed before a
   subscriber is created, `stream.Subscribe()` returns an already-closed
   channel. Range over it safely; it will exit immediately.
6. **NDJSON vs SSE vs turn_complete.** The HTTP conduit demonstrates two valid
   patterns: NDJSON streaming over a request/response connection, and SSE over a
   persistent ambient connection. The TUI subscribes to `"turn_complete"` for
   batched rendering. Choose the pattern that matches your transport.
## References

- `AGENTS.md` — ore architectural boundaries and the Conduit/Library vs.
  Application contract.
- `x/conduit/doc.go` — standard conduit contract documentation (constructor,
  Descriptor, blocking Start, graceful shutdown).
- `./SKELETON.md` — compilable reference skeleton with contract cross-references.
- `x/conduit/http/` — HTTP conduit reference (NDJSON streaming, SSE,
  embedded web UI, RESTful session endpoints).
- `x/conduit/tui/` — TUI conduit reference (Bubble Tea, turn_complete
  subscription, channel-based Process loop).
- `go/` skill — Go conventions (functional options, table-driven tests,
  error wrapping with `fmt.Errorf`, `log/slog`).
- `.plans/standardize-conduit-patterns.md` — repo-internal plan that
  standardized the HTTP `Descriptor` export and the `x/conduit/doc.go`
  conduit contract.
- `./README_EXAMPLE.md` — filled-out reference README demonstrating the
  standardized sections for composer-facing documentation.

> **Note:** This skill is a living document. After implementing a new conduit,
> review whether any pattern you discovered should be added here, and whether
> the `README_EXAMPLE.md` template should be updated.
