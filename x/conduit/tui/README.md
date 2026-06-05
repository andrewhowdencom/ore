# TUI Conduit

Terminal user interface conduit for the ore framework, built with [Bubble Tea](https://github.com/charmbracelet/bubbletea).

## Quick Start

```go
import "github.com/andrewhowdencom/ore/x/conduit/tui"

// Create a TUI conduit and start it.
t, err := tui.New(manager, tui.WithName("my-app"))
if err != nil {
    // handle error
}
err = t.Start(ctx)
```

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

## Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `Ctrl+O` | Toggle expansion of the latest assistant turn's details — tool calls and reasoning (compact by default; resets after each new turn) |
| `Ctrl+C` | Quit |
| `Shift+Enter` | Insert newline in the input box |
| `Ctrl+J`      | Insert newline (alternative for terminals that don't pass Shift+Enter) |

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

For full API documentation, run `go doc ./x/conduit/tui`.
