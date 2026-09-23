---
name: conduit
description: |
  Implements a new ore I/O conduit package under x/conduit/<name>/ using the
  functional-options constructor pattern, exported Descriptor for discovery, and
  blocking Start(ctx) lifecycle. Dumb pipe that translates external system events
  (HTTP, TUI, chat bot, webhook) into events for an application-owned session,
  subscribes to broadcast session output, and routes text/reasoning/image
  artifacts back to external systems. Compatible with the broadcast
  multi-conduit model. Does NOT handle cognitive orchestration,
  provider invocation, or turn-loop management.
---

# Ore Conduit

## When to Use

This skill is triggered **ONLY** when implementing a **NEW** I/O conduit package
under `x/conduit/<name>/` or modifying an existing conduit implementation.

Do NOT use this skill for:
- Core package work (`artifact/`, `loop/`, `junk/`, `provider/`, `state/`, `thread/`, `cognitive/`)
- Modifying ore `AGENTS.md` or repository-level documentation
- Go language or tooling questions (see `go/` skill instead)
- Ore architectural philosophy or package boundary decisions (see `AGENTS.md`)

## What is a Conduit

An ore conduit is a dumb pipe that translates events between an external system
and the ore framework. It is not a "UI" in the narrow sense, nor is it a
cognitive agent. A conduit's only job is ingress (mapping external events into
`session.Event` values for the application to submit) and egress
(subscribing to session output and routing assistant artifacts
back to the external system).

Conduits must never import `cognitive/` packages, invoke `provider.Invoke()` or
an engine directly, or manage turn loops. Those are application-level concerns
composed in `examples/` or `cmd/` packages. See `AGENTS.md` for the full
Conduit/Library vs. Application boundary.

## Execution Procedure

Follow these steps in order. Do not skip or reorder.

1. **Create package `x/conduit/<name>/`** with its own `go.mod`. Use
   `replace github.com/andrewhowdencom/ore => ../../..` to link the core module.
   Then add the new module to the root `Taskfile.yml` `includes:` block so
   `task validate` covers it (follow the pattern of existing entries).
2. **Implement `conduit.Conduit`** — a type with exactly one method:
   `Start(ctx context.Context) error`.
3. **Accept `*session.Session`** via the constructor using the functional options
   pattern:
   ```go
   func New(sess *session.Session, opts ...Option) (conduit.Conduit, error)
   ```
   Validate `sess != nil`; return an error if nil. Functional options can
   override defaults but must not be required.
4. **Leave session ownership to the application.** The application creates or
   attaches the session, registers it with an engine, and passes it to the
   conduit. The conduit must not create, attach, or run sessions itself.
5. **Subscribe to output events** from the session inside `Start()`:
   ```go
   outputCh := sess.Subscribe("turn_complete")
   ```
   For streaming conduits, subscribe to artifact kinds directly
   (`"text_delta"`, `"reasoning_delta"`, etc.).
   The stream uses a FanOut broadcast model. Multiple conduits can subscribe
   concurrently; each receives all events independently.
6. **Capture your delivery mechanism in the subscriber closure.** The subscriber
   goroutine must close over the external-system client (HTTP writer, Slack
   client, TUI program, etc.). Destination routing is **NOT** carried in the
   provenance attached to an event context.
   If delivery to the external system fails, log the error (non-fatal) and
   continue. Optionally render a failure message if the transport supports it.
7. **Expose external input as session events.** Conduits that produce input
   expose a buffered `Events() <-chan session.Event`. Map external messages to
   `session.UserMessageEvent` values with provenance attached to their context.
   The application consumes the channel and calls `engine.Submit`; the conduit
   never invokes a provider or drives the turn loop. Cancellation is propagated
   through the event context configured by an option such as
   `WithEventContext`.
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
10. **Add table-driven tests** with a test `session.Session` or
    `httptest.Server`. Verify `Start()` blocks, `Descriptor` is exported, and
    the constructor rejects a nil session.
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
- [ ] Constructor accepts `*session.Session` and validates non-nil
- [ ] `Start(ctx)` blocks until `ctx.Done()`
- [ ] Subscribes to output events before blocking
- [ ] Exposes external inputs as `session.Event` values for the application
- [ ] Passes `go test -race ./...`
- [ ] Attaches source provenance to emitted event contexts
- [ ] `README.md` is present with all required sections (see `./README_EXAMPLE.md`)
- [ ] Module is registered in root `Taskfile.yml` `includes:` block so
  `task validate` covers it

## Boolean Guards

If any of the following are true, **STOP** and reassess:

- ⚠️ **IF** importing `cognitive/` → STOP. Conduits are dumb pipes. Cognitive
  patterns (ReAct, chain-of-thought) belong in the application layer, not the
  conduit.
- ⚠️ **IF** calling `provider.Invoke()` or an engine directly → STOP. Emit a
  `session.Event`; the application owns submission and turn processing.
- ⚠️ **IF** managing turn loops, tool execution loops, or ReAct logic → STOP.
  That is the `cognitive/` package's responsibility or the application's
  `TurnProcessor`.
- ⚠️ **IF** putting destination routing metadata (channel ID, thread ID, email
  address) into event provenance → STOP. Capture the delivery mechanism
  in the subscriber closure. `Provenance` is for source metadata only.
- ⚠️ **IF** the conduit requires mandatory constructor options (not just
  functional options with defaults) → STOP. Conduits should use functional
  options with sensible defaults so they compose easily in hand-written main.go
  files.

## Gotchas

1. **Subscriber backpressure.** `sess.Subscribe()` channels have a fixed buffer
   of 100 events. Slow subscribers silently drop events. Design for idempotency
   or tolerate missing deltas. Do not assume reliable delivery.
2. **Application-owned submission.** A conduit emits `session.Event` values;
   the application decides how to submit them to its engine. Do not call the
   provider or embed engine orchestration in the conduit.
3. **Provenance is source-only.** Attach it with `loop.WithProvenance`; it is
   not destination routing metadata (channel IDs, thread timestamps, email
   addresses). Capture the delivery mechanism in the subscriber closure.
4. **MarshalArtifact is hardcoded.** `x/conduit/http/types.go` has a fixed switch
   for core artifact kinds. If your conduit defines custom artifact types and
   uses HTTP transport, you must extend the marshal/unmarshal functions or
   handle serialization yourself.
5. **Closed subscription on a closed session.** If a session is closed before a
   subscriber is created, `sess.Subscribe()` returns an already-closed
   channel. Range over it safely; it will exit immediately.
6. **NDJSON vs SSE vs deltas.** The HTTP conduit demonstrates request/response
   and persistent streaming patterns. The TUI subscribes to text and reasoning
   deltas plus completed turns. Choose the event set that matches the transport.
7. **Missing from `Taskfile.yml`.** Every workspace module with its own `go.mod`
   must be listed in the root `Taskfile.yml` `includes:` block pointing to
   `Taskfile.lib.yml`. If omitted, `task validate` silently skips the module
   and compilation errors or test failures are hidden from CI.
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
