package tui

import (
	"github.com/andrewhowdencom/ore/x/conduit/tui/theme"
	"github.com/charmbracelet/glamour"
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
// The glamour StyleConfig comes from the supplied *theme.Theme, so the
// theme is the single source of truth for both the structural rules of
// markdown rendering and the colour palette. Mode selection (dark vs
// light) is performed by the caller via theme.Auto() or one of the
// explicit factory functions (theme.Dark, theme.Light).
type glamourMarkdownRenderer struct {
	theme *theme.Theme
}

// newGlamourMarkdownRenderer creates a renderer that uses the supplied
// theme's glamour StyleConfig. The caller is responsible for selecting
// the appropriate theme (typically via theme.Auto() at construction
// time, or by accepting a *theme.Theme from tui.WithTheme(...)).
func newGlamourMarkdownRenderer(th *theme.Theme) *glamourMarkdownRenderer {
	return &glamourMarkdownRenderer{theme: th}
}

func (r glamourMarkdownRenderer) Render(text string, width int) (string, error) {
	rnd, err := glamour.NewTermRenderer(
		glamour.WithStyles(r.theme.GlamourStyle),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return "", err
	}
	return rnd.Render(text)
}
