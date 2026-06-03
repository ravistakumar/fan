package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestRunCommandEndToEnd builds a fake "claude" on PATH that writes a file
// named after the prompt's last word, then runs `fan run --each` over two
// items in a temp repo and asserts both tasks merge onto the integration branch.
func TestRunCommandEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// Set up a temp git repo with one seed commit.
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "-A")
	gitRun(t, repo, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "seed")

	// Build a fake "claude" shell script. The real agent is called as:
	//   claude -p "<prompt>"
	// so $1="-p" and $2=the prompt text. The template "Create {}" with item
	// "x.txt" yields prompt "Create x.txt"; awk prints the last word ("x.txt"),
	// and the script writes that file in the working directory (the worktree).
	bin := t.TempDir()
	script := "#!/bin/sh\nlast=$(echo \"$2\" | awk '{print $NF}')\necho done > \"$last\"\n"
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	// Chdir into the temp repo so vcs.Open(".") finds it; restore on exit.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd) //nolint:errcheck
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	cmd := newRunCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--each", "x.txt,y.txt", "--agent", "claude", "Create {}"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("run: %v\n%s", err, out.String())
	}

	if !bytes.Contains(out.Bytes(), []byte("2 merged")) {
		t.Errorf("expected 2 merged in output:\n%s", out.String())
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
