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
//
// The renderer loads one of two embedded style files (darkStyle or lightStyle)
// which are tweaked copies of glamour's built-in themes with document.margin
// set to 0. Style selection is performed at construction time based on runtime
// terminal detection: non-terminal defaults to dark; terminal with dark
// background selects dark; otherwise light.
type glamourMarkdownRenderer struct {
	styleBytes []byte
}

// newGlamourMarkdownRenderer creates a renderer with auto-detected style.
// It uses term.IsTerminal on os.Stdout and termenv.HasDarkBackground to
// decide between the embedded dark and light styles.
func newGlamourMarkdownRenderer() *glamourMarkdownRenderer {
	return newGlamourMarkdownRendererWithDetectors(
		func() bool { return term.IsTerminal(int(os.Stdout.Fd())) },
		termenv.HasDarkBackground,
	)
}

func newGlamourMarkdownRendererWithDetectors(isTerminal func() bool, hasDarkBackground func() bool) *glamourMarkdownRenderer {
	var style []byte
	if !isTerminal() {
		// Not a terminal; default to dark style.
		style = darkStyle
	} else if hasDarkBackground() {
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