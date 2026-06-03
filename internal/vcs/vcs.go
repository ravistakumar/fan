// Package vcs wraps the git operations fan needs: opening a repo, creating and
// removing worktrees, committing all changes in a worktree, creating branches,
// and cherry-picking onto an integration worktree. All operations shell out to
// the git binary; no operation touches the user's primary checkout.
package vcs

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Repo is a handle to a git repository identified by its top-level directory.
type Repo struct {
	root string
}

// Open verifies dir is inside a git work tree and returns a Repo rooted at its
// top level.
func Open(dir string) (*Repo, error) {
	out, err := git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("not a git repository: %w", err)
	}
	return &Repo{root: strings.TrimSpace(out)}, nil
}

// Root returns the repository's top-level directory.
func (r *Repo) Root() string { return r.root }

// Head returns the current commit SHA of the repository's checked-out branch.
func (r *Repo) Head() (string, error) {
	out, err := git(r.root, "rev-parse", "HEAD")
	return strings.TrimSpace(out), err
}

// AddWorktree creates a detached worktree at path checked out at base.
func (r *Repo) AddWorktree(path, base string) error {
	_, err := git(r.root, "worktree", "add", "--detach", path, base)
	return err
}

// CreateWorktreeBranch creates a worktree at path on a new branch pointing at
// base. This is where cherry-picks are applied, keeping the user's checkout
// untouched.
func (r *Repo) CreateWorktreeBranch(path, branch, base string) error {
	_, err := git(r.root, "worktree", "add", "-b", branch, path, base)
	return err
}

// RemoveWorktree force-removes the worktree at path.
func (r *Repo) RemoveWorktree(path string) error {
	_, err := git(r.root, "worktree", "remove", "--force", path)
	return err
}

// CommitAll stages everything in the worktree at dir and commits it. changed is
// false (with no commit and an empty sha) when there is nothing to commit.
func (r *Repo) CommitAll(dir, msg string) (sha string, changed bool, err error) {
	if _, err = git(dir, "add", "-A"); err != nil {
		return "", false, err
	}
	status, err := git(dir, "status", "--porcelain")
	if err != nil {
		return "", false, err
	}
	if strings.TrimSpace(status) == "" {
		return "", false, nil
	}
	if _, err = git(dir, "commit", "-q", "-m", msg); err != nil {
		return "", false, err
	}
	out, err := git(dir, "rev-parse", "HEAD")
	if err != nil {
		return "", false, err
	}
	return strings.TrimSpace(out), true, nil
}

// CreateBranch points a new branch name at sha, so a queued task's work is
// preserved as a reviewable ref.
func (r *Repo) CreateBranch(name, sha string) error {
	_, err := git(r.root, "branch", name, sha)
	return err
}

// CherryPick applies sha onto the worktree at intDir. clean is false when the
// pick conflicts; in that case the pick is aborted so intDir is left usable for
// the next pick.
func (r *Repo) CherryPick(intDir, sha string) (clean bool, err error) {
	if _, err := git(intDir, "cherry-pick", "--allow-empty", "-x", sha); err != nil {
		_, _ = git(intDir, "cherry-pick", "--abort")
		return false, nil
	}
	return true, nil
}

// git runs a git command in dir and returns its stdout, or an error carrying
// stderr.
func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=fan", "GIT_AUTHOR_EMAIL=fan@local",
		"GIT_COMMITTER_NAME=fan", "GIT_COMMITTER_EMAIL=fan@local")
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return string(out), fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(ee.Stderr)))
		}
		return string(out), fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}
