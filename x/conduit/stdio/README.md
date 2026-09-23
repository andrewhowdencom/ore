# stdio Conduit

Single-shot, Unix-filter-style ore conduit for stdin/stdout/file I/O.

## Installation

```bash
go get github.com/andrewhowdencom/ore/x/conduit/stdio@latest
```

## Overview

The stdio conduit reads one user message from an `io.Reader`, submits it to a
bound `session.Session`, streams assistant artifacts to an `io.Writer`, and
returns when the turn finishes. The application owns the session, registers it
with an engine, and drives inference; the conduit only handles terminal I/O.

Unlike long-running conduits, `Start` deliberately returns after one turn so it
can be used in CLI pipelines and Unix filters.

## Capabilities

`stdio.Descriptor` advertises the following static capabilities:

- `event-source` — submits input as a user turn on the bound session.
- `render-markdown` — renders assistant output as Markdown-compatible text.
- `accept-text` — accepts one raw text payload from an `io.Reader`.

Descriptors are descriptive metadata rather than runtime feature negotiation.

## Composition

```go
func New(sess *session.Session, opts ...Option) (conduit.Conduit, error)
```

The session must already be registered with an engine that will process its
events. A minimal conduit setup looks like this; see the example applications
for complete engine construction and lifecycle management.

```go
c, err := stdio.New(sess,
    stdio.WithInput(os.Stdin),
    stdio.WithOutput(os.Stdout),
    stdio.WithStderr(os.Stderr),
)
if err != nil {
    return err
}

if err := c.Start(ctx); err != nil {
    return err
}
```

Thread lookup and hydration are application responsibilities. Construct or
attach the desired session before calling `New`; the conduit has no thread-ID
option and does not create sessions itself.

## Configuration

| Option | Default | Description |
|---|---|---|
| `WithInput(io.Reader)` | `os.Stdin` | Source of the single user message. |
| `WithOutput(io.Writer)` | `os.Stdout` | Destination for assistant output. |
| `WithStderr(io.Writer)` | `os.Stderr` | Destination for notices and other out-of-band output. |
| `WithTracer(trace.Tracer)` | no tracing | Tracer used for the `stdio.turn` server span. |

## Runtime Semantics

`Start` subscribes to the bound session before reading input. It then submits a
user turn with `session.Submit` and waits for the engine-driven session stream
to report completion or failure.

The renderer handles:

- `text_delta` as plain text;
- `reasoning_delta` in a `reasoning` Markdown fence;
- `tool_call_delta` and complete tool calls in `tool-call` Markdown fences;
- notices on the configured stderr writer;
- turn errors as returned errors.

Events carrying non-empty provenance other than `stdio` are ignored, preventing
the conduit from rendering output initiated by another conduit sharing the
session. Events without provenance are accepted.

The bound session is closed after submission. `Start` returns after the
subscriber finishes, when the context is cancelled, or when an error occurs.

## Error Handling

`Start` returns an error when it cannot read input, receives empty input, cannot
submit the user turn, observes a turn error, or is cancelled before completion.
Writes currently use the configured writers directly; individual write errors
are not surfaced.
