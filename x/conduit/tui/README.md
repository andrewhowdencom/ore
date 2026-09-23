# TUI Conduit

Terminal user interface conduit for the ore framework, built with [Bubble Tea](https://github.com/charmbracelet/bubbletea).

## Quick Start

```go
import (
    "github.com/andrewhowdencom/ore/ledger"
    "github.com/andrewhowdencom/ore/session"
    "github.com/andrewhowdencom/ore/x/conduit/tui"
)

// 1. Construct the session (typically via an engine.Engine — see
//    examples/tui-chat for the full wiring).
sess := session.New("thread-id", ledger.NewThread())
// Seed any default metadata before constructing the TUI.
sess.SetMetadata("thread_id", sess.ID())

// 2. Construct the TUI conduit.
tuiConduit, err := tui.New(sess, tui.WithName("my-app"))
if err != nil {
    // handle error
}

// 3. The application pumps Events() into an engine.Engine.
go func() {
    for evt := range tuiConduit.(*tui.TUI).Events() {
        eng.Submit(ctx, sess.ID(), evt)
    }
}()

// 4. Start blocks until the user quits or ctx is cancelled.
err = tuiConduit.Start(ctx)
```

The TUI is a **dumb pipe**: it does not invoke the provider, does not own
the session lifecycle, and does not manage the turn loop. It subscribes to
session output events and routes them into the Bubble Tea program. User
actions produce typed messages on the channel returned by `Events()` for the
application to consume via `engine.Engine.Submit`. Ctrl+C and Esc cancel the
context of the current message.

## Capabilities

`tui.Descriptor` advertises the following static capabilities:

- `event-source` — emits user messages through `Events()`.
- `show-status` — renders structured session and lifecycle metadata.
- `render-turn` — renders completed assistant turns and history.
- `render-markdown` — renders assistant text and reasoning as Markdown.
- `audio-notification` — signals completion and failure with the terminal bell.

Descriptors are descriptive metadata rather than runtime feature negotiation.
The TUI also renders text and reasoning deltas incrementally, but its current
descriptor does not advertise `render-delta`.

## Window Title

The TUI sets the terminal window title dynamically based on the
session lifecycle state. The default name is `Ore`; pass
`WithName("your-app")` to override it.

| Phase | Title |
|-------|-------|
| submitted, streaming | `<name> [...]` |
| done, initial | `<name> [ok]` |
| error | `<name> [err]` |

This makes it easy to distinguish multiple ore sessions in tmux or
a terminal multiplexer.

## Cancellation

Use `WithEventContext` to provide the parent context carried by events emitted
from the TUI. The TUI derives a cancellable child for each submitted event;
Esc or Ctrl+C cancels that context so in-flight engine work can unwind:

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

tuiConduit, _ := tui.New(sess, tui.WithEventContext(ctx))
go tuiConduit.Start(ctx)
```

The lifetime of the TUI remains governed separately by the context passed to
`Start`. If `WithEventContext` is omitted, the TUI uses the `Start` context for
both purposes.

## Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `Ctrl+O` | Toggle expansion of the latest assistant turn's details — tool calls and reasoning (compact by default; resets after each new turn) |
| `Ctrl+C` | Cancel the current event context and quit |
| `Esc` | Cancel the current event context (does not quit) |
| `Shift+Enter` | Insert newline in the input box |
| `Ctrl+J` | Insert newline (alternative for terminals that don't pass Shift+Enter) |

## Mouse Scrolling

The TUI captures mouse events so the scroll wheel scrolls the conversation
history in the same way as `PgUp` / `PgDown`. This is enabled by default and
can be used in any terminal that supports mouse mode (e.g. iTerm2, Windows
Terminal, tmux with `mouse on`).

## Design

Tool calls, tool results, and reasoning blocks are rendered in a compact
single-line form by default to keep the conversation readable within limited
terminal space. Users can press `Ctrl+O` to temporarily expand the latest
assistant turn's details, inspecting full tool arguments, error messages, or
reasoning content. Historical detail blocks always remain compact.

## Streaming Model

The TUI subscribes to `text_delta`, `reasoning_delta`, `tool_call`,
`tool_result`, and `turn_complete` events. Assistant output is rendered
incrementally as deltas arrive, with a 16ms debounced render tick that
reduces flicker while preserving low latency.

## Refreshing after Thread Replacement

If the underlying conversation state is replaced (e.g. after compaction
via `thread.Replace`), call `ReloadHistory` on the TUI to rebuild the
conversation view from the new turn slice. This must be done after
`Start` has been called so the Bubble Tea program is running.

```go
tuiConduit.(*tui.TUI).ReloadHistory(sess.Turns(), boundaryInfo)
```

## API Migration Notes

The TUI previously took a `*junk.Manager` as its constructor argument and
managed session lifecycle (create / attach) internally. The current
session-based API moves that responsibility to the application:

- **Before:** `tui.New(mgr, tui.WithThreadID("abc"))` — the TUI attached
  to thread `abc` on `Start`.
- **After:** the application constructs the session (via
  `engine.Engine`'s session registry) and passes it to `tui.New(sess)`.

This matches the framework-wide dumb-pipe convention: conduits do not own
session lifecycle or manage the turn loop. The stdio conduit now follows the
same session-based pattern. Slack and Telegram still use the legacy
`*junk.Manager` pattern; HTTP uses an application-supplied `Backend` interface.

For full API documentation, run `go doc ./x/conduit/tui`.
