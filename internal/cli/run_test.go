package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/ravistakumar/fan/internal/run"
	"github.com/ravistakumar/fan/internal/tui"
)

func TestResolveEachItems(t *testing.T) {
	// Comma list passes through untouched.
	got, err := resolveEachItems("a.ts,b.ts")
	if err != nil {
		t.Fatalf("resolveEachItems: %v", err)
	}
	if len(got) != 2 || got[0] != "a.ts" || got[1] != "b.ts" {
		t.Errorf("got %v", got)
	}
}

func TestResolveEachItemsRejectsEmpty(t *testing.T) {
	if _, err := resolveEachItems(""); err == nil {
		t.Fatal("expected error for empty --each")
	}
}

func TestFilterByOnly(t *testing.T) {
	ids := []string{"a", "b", "c"}
	got := filterIDs(ids, "a,c")
	if strings.Join(got, ",") != "a,c" {
		t.Errorf("got %v", got)
	}
	if all := filterIDs(ids, ""); len(all) != 3 {
		t.Errorf("empty --only should keep all, got %v", all)
	}
}

func TestChooseReporterFallsBackToTextWhenPlain(t *testing.T) {
	// plain=true forces the text reporter even if stderr were a terminal.
	r := chooseReporter(os.Stderr, true, func() {})
	if _, ok := r.(*run.TextReporter); !ok {
		t.Errorf("plain should select TextReporter, got %T", r)
	}
}

func TestChooseReporterUsesTextForNonTTY(t *testing.T) {
	// A pipe is not a TTY → text reporter regardless of plain.
	_, wr, _ := os.Pipe()
	defer wr.Close()
	r := chooseReporter(wr, false, func() {})
	if _, ok := r.(*run.TextReporter); !ok {
		t.Errorf("non-TTY should select TextReporter, got %T", r)
	}
}

func TestChooseReporterUsesTUIForTTY(t *testing.T) {
	// Allocating a real PTY isn't available in CI, so skip unless stderr
	// happens to be an interactive terminal; when it is, the TTY path must
	// select the TUI reporter.
	if !tui.IsTerminal(os.Stderr) {
		t.Skip("no TTY available in this environment")
	}
	r := chooseReporter(os.Stderr, false, func() {})
	if _, ok := r.(*tui.Reporter); !ok {
		t.Errorf("TTY should select tui.Reporter, got %T", r)
	}
}
