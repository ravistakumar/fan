package tui

import (
	"os"

	"golang.org/x/term"
)

// IsTerminal reports whether f is an interactive terminal. The TUI renders only
// when this is true for the stream it draws on (stderr); otherwise the caller
// falls back to the text reporter.
func IsTerminal(f *os.File) bool {
	return f != nil && term.IsTerminal(int(f.Fd()))
}
