package theme

import (
	"os"

	"github.com/muesli/termenv"
	"golang.org/x/term"
)

// Auto returns a Theme appropriate for the current terminal. The detection
// mirrors what x/conduit/tui/markdown.go used to do at renderer
// construction time: a non-TTY defaults to Dark (glamour's chroma colours
// assume a dark background), a TTY with a dark background selects Dark,
// and a TTY with a light background selects Light.
//
// The decision is made at call time, not at process start, so a process
// whose terminal background changes (rare, but possible) re-evaluates
// each call. Callers that need stable behaviour across the lifetime of a
// process should call Dark or Light explicitly and pass the result to
// tui.WithTheme(...).
func Auto() *Theme {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return Dark()
	}
	if termenv.HasDarkBackground() {
		return Dark()
	}
	return Light()
}
