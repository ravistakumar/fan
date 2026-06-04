package tui

import (
	"os"
	"testing"
)

func TestIsTerminalFalseForPipe(t *testing.T) {
	rd, wr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer rd.Close()
	defer wr.Close()
	if IsTerminal(wr) {
		t.Error("a pipe write end is not a terminal")
	}
}

func TestIsTerminalFalseForRegularFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "f")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if IsTerminal(f) {
		t.Error("a regular file is not a terminal")
	}
}
