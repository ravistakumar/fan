package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNewSupportedAgents(t *testing.T) {
	for _, name := range Supported {
		if _, err := New(name); err != nil {
			t.Errorf("New(%q): %v", name, err)
		}
	}
	if _, err := New("nope"); err == nil {
		t.Error("New(nope): expected error")
	}
}

func TestIsSupported(t *testing.T) {
	if !IsSupported("grok") {
		t.Error("grok should be supported")
	}
	if IsSupported("nope") {
		t.Error("nope should not be supported")
	}
}

// TestRunInDir uses a stand-in command ("sh") to prove Run executes in the
// given directory and returns stdout. It does not exercise a real agent.
func TestRunInDir(t *testing.T) {
	dir := t.TempDir()
	a := &cmdAgent{name: "fake", command: "sh", runArgs: []string{"-c"}}
	out, err := a.Run(context.Background(), "pwd", dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	resolved, _ := filepath.EvalSymlinks(dir)
	got, _ := filepath.EvalSymlinks(trim(out))
	if got != resolved {
		t.Errorf("ran in %q, want %q", got, resolved)
	}
}

func TestRunSurfacesStderrOnFailure(t *testing.T) {
	a := &cmdAgent{name: "fake", command: "sh", runArgs: []string{"-c"}}
	_, err := a.Run(context.Background(), "echo boom >&2; exit 3", t.TempDir())
	if err == nil {
		t.Fatal("expected error from failing command")
	}
}

func trim(s string) string { return string([]byte(stringsTrimSpace(s))) }

func stringsTrimSpace(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == ' ' || s[len(s)-1] == '\t' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

var _ = os.Stdout
