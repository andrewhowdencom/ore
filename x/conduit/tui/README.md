# TUI Conduit

Terminal user interface conduit for the ore framework, built with [Bubble Tea](https://github.com/charmbracelet/bubbletea).

## Quick Start

```go
import "github.com/andrewhowdencom/ore/x/conduit/tui"

// Create a TUI conduit and start it.
t, err := tui.New(manager)
if err != nil {
    // handle error
}
err = t.Start(ctx)
```

## Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `Ctrl+O` | Toggle expansion of the latest assistant turn's details — tool calls and reasoning (compact by default; resets after each new turn) |
| `Ctrl+C` | Quit |
| `Shift+Enter` | Insert newline in the input box |

## Design

Tool calls, tool results, and reasoning blocks are rendered in a compact
single-line form by default to keep the conversation readable within limited
terminal space. Users can press `Ctrl+O` to temporarily expand the latest
assistant turn's details, inspecting full tool arguments, error messages, or
reasoning content. Historical detail blocks always remain compact.

For full API documentation, run `go doc ./x/conduit/tui`.
