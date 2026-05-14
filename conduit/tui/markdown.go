package tui

import (
	"os"

	"github.com/charmbracelet/glamour"
	"github.com/muesli/termenv"
	"golang.org/x/term"
)

// markdownRenderer converts Markdown source text into ANSI-styled terminal
// output. It is an interface so tests can inject mock implementations that
// simulate success, failure, or specific styling behaviour without calling
// the heavy glamour library.
type markdownRenderer interface {
	Render(text string, width int) (string, error)
}

// glamourMarkdownRenderer is the production implementation that delegates to
// charmbracelet/glamour. It creates a new TermRenderer per call because
// glamour renderers are not safe for concurrent reuse and the Bubble Tea model
// runs on a single goroutine anyway.
type glamourMarkdownRenderer struct {
	styleBytes []byte
}

func newGlamourMarkdownRenderer() *glamourMarkdownRenderer {
	var style []byte
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		// Not a terminal; default to dark style.
		style = darkStyle
	} else if termenv.HasDarkBackground() {
		style = darkStyle
	} else {
		style = lightStyle
	}
	return &glamourMarkdownRenderer{styleBytes: style}
}

func (r glamourMarkdownRenderer) Render(text string, width int) (string, error) {
	rnd, err := glamour.NewTermRenderer(
		glamour.WithStylesFromJSONBytes(r.styleBytes),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return "", err
	}
	return rnd.Render(text)
}