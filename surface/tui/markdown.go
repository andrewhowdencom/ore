package tui

import "github.com/charmbracelet/glamour"

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
type glamourMarkdownRenderer struct{}

func (glamourMarkdownRenderer) Render(text string, width int) (string, error) {
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return "", err
	}
	return r.Render(text)
}
