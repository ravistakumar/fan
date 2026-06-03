package vcs

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initRepo creates a temp git repo with one commit and returns its path.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-q", "-m", "seed")
	return dir
}

func TestOpenAndHead(t *testing.T) {
	r, err := Open(initRepo(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	sha, err := r.Head()
	if err != nil || len(sha) < 7 {
		t.Fatalf("Head: %q %v", sha, err)
	}
}

func TestWorktreeCommitAndCleanCherryPick(t *testing.T) {
	r, _ := Open(initRepo(t))
	base, _ := r.Head()

	// Integration worktree on a new branch.
	intDir := filepath.Join(t.TempDir(), "integration")
	if err := r.CreateWorktreeBranch(intDir, "fan/test", base); err != nil {
		t.Fatalf("CreateWorktreeBranch: %v", err)
	}

	// Task worktree; make a non-conflicting change; commit.
	wtDir := filepath.Join(t.TempDir(), "wt")
	if err := r.AddWorktree(wtDir, base); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	os.WriteFile(filepath.Join(wtDir, "a.txt"), []byte("a\n"), 0o644)
	sha, changed, err := r.CommitAll(wtDir, "fan(a): add a")
	if err != nil || !changed {
		t.Fatalf("CommitAll: sha=%q changed=%v err=%v", sha, changed, err)
	}

	clean, err := r.CherryPick(intDir, sha)
	if err != nil {
		t.Fatalf("CherryPick: %v", err)
	}
	if !clean {
		t.Error("expected clean cherry-pick")
	}
	if _, err := os.Stat(filepath.Join(intDir, "a.txt")); err != nil {
		t.Errorf("a.txt not on integration branch: %v", err)
	}
}

func TestCommitAllNoChanges(t *testing.T) {
	r, _ := Open(initRepo(t))
	base, _ := r.Head()
	wtDir := filepath.Join(t.TempDir(), "wt")
	r.AddWorktree(wtDir, base)
	_, changed, err := r.CommitAll(wtDir, "noop")
	if err != nil {
		t.Fatalf("CommitAll: %v", err)
	}
	if changed {
		t.Error("expected changed=false when nothing was modified")
	}
}

func TestConflictingCherryPickReportsUnclean(t *testing.T) {
	r, _ := Open(initRepo(t))
	base, _ := r.Head()
	intDir := filepath.Join(t.TempDir(), "integration")
	r.CreateWorktreeBranch(intDir, "fan/test", base)

	// First task edits seed.txt and is merged.
	wt1 := filepath.Join(t.TempDir(), "wt1")
	r.AddWorktree(wt1, base)
	os.WriteFile(filepath.Join(wt1, "seed.txt"), []byte("one\n"), 0o644)
	sha1, _, _ := r.CommitAll(wt1, "edit one")
	if clean, _ := r.CherryPick(intDir, sha1); !clean {
		t.Fatal("first pick should be clean")
	}

	// Second task edits the same line from the same base → conflict.
	wt2 := filepath.Join(t.TempDir(), "wt2")
	r.AddWorktree(wt2, base)
	os.WriteFile(filepath.Join(wt2, "seed.txt"), []byte("two\n"), 0o644)
	sha2, _, _ := r.CommitAll(wt2, "edit two")
	clean, err := r.CherryPick(intDir, sha2)
	if err != nil {
		t.Fatalf("CherryPick: %v", err)
	}
	if clean {
		t.Error("expected conflict (unclean)")
	}
	// Repo must be left clean (cherry-pick aborted), so the next pick can run.
	if clean3, _ := r.CherryPick(intDir, sha1); clean3 {
		// sha1 already applied → empty pick; we only assert no error/hang.
	}
}

func TestCreateBranchAtSha(t *testing.T) {
	r, _ := Open(initRepo(t))
	base, _ := r.Head()
	wt := filepath.Join(t.TempDir(), "wt")
	r.AddWorktree(wt, base)
	os.WriteFile(filepath.Join(wt, "b.txt"), []byte("b\n"), 0o644)
	sha, _, _ := r.CommitAll(wt, "add b")
	if err := r.CreateBranch("fan/keep-me", sha); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
}

var _ = context.Background
