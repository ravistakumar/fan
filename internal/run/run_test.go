package run

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ravistakumar/fan/internal/agent"
	"github.com/ravistakumar/fan/internal/result"
	"github.com/ravistakumar/fan/internal/task"
	"github.com/ravistakumar/fan/internal/vcs"
)

// fakeAgent writes a file named after the task id, simulating an edit. If fail
// is true it returns an error without writing.
type fakeAgent struct {
	name string
	file string // file to create, relative to dir
	body string
	fail bool
}

func (f fakeAgent) Name() string      { return f.name }
func (f fakeAgent) Available() bool   { return true }
func (f fakeAgent) Run(_ context.Context, _ string, dir string) (string, error) {
	if f.fail {
		return "", os.ErrPermission
	}
	return "", os.WriteFile(filepath.Join(dir, f.file), []byte(f.body), 0o644)
}

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
	_ = os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("seed\n"), 0o644)
	run("add", "-A")
	run("commit", "-q", "-m", "seed")
	return dir
}

func TestRunMergesCleanTasks(t *testing.T) {
	repoDir := initRepo(t)
	repo, _ := vcs.Open(repoDir)

	tasks := []task.Task{
		{ID: "a", Prompt: "make a", Agent: "fake", Gate: "true"},
		{ID: "b", Prompt: "make b", Agent: "fake", Gate: "true"},
	}
	files := map[string]string{"a": "a.txt", "b": "b.txt"}

	r := Runner{
		Repo:     repo,
		WorkRoot: t.TempDir(),
		Reporter: NewTextReporter(os.Stderr),
		Now:      func() string { return "2026-06-03-1432" },
		// AgentFor injects a per-task fake agent keyed by task id.
		AgentFor: func(tk task.Task) (agent.Agent, error) {
			return fakeAgent{name: "fake", file: files[tk.ID], body: tk.ID + "\n"}, nil
		},
	}

	outcomes, branch, err := r.Run(context.Background(), tasks, 2, true)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if branch != "fan/2026-06-03-1432" {
		t.Errorf("branch = %q", branch)
	}
	for _, o := range outcomes {
		if o.Status != result.Merged {
			t.Errorf("task %s: status %q, want merged", o.Task.ID, o.Status)
		}
	}
}

func TestRunQueuesGateFailures(t *testing.T) {
	repo, _ := vcs.Open(initRepo(t))
	tasks := []task.Task{{ID: "a", Prompt: "make a", Agent: "fake", Gate: "false"}}
	r := Runner{
		Repo: repo, WorkRoot: t.TempDir(),
		Reporter: NewTextReporter(os.Stderr),
		Now:      func() string { return "ts" },
		AgentFor: func(tk task.Task) (agent.Agent, error) {
			return fakeAgent{name: "fake", file: "a.txt", body: "a\n"}, nil
		},
	}
	outcomes, _, err := r.Run(context.Background(), tasks, 1, false)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcomes[0].Status != result.QueuedGateFail {
		t.Errorf("status = %q, want queued: gate-fail", outcomes[0].Status)
	}
	if outcomes[0].Branch == "" {
		t.Error("gate-failed task should keep a branch")
	}
}

func TestRunRecordsAgentErrors(t *testing.T) {
	repo, _ := vcs.Open(initRepo(t))
	tasks := []task.Task{{ID: "a", Prompt: "p", Agent: "fake", Gate: "true"}}
	r := Runner{
		Repo: repo, WorkRoot: t.TempDir(),
		Reporter: NewTextReporter(os.Stderr),
		Now:      func() string { return "ts" },
		AgentFor: func(tk task.Task) (agent.Agent, error) {
			return fakeAgent{name: "fake", fail: true}, nil
		},
	}
	outcomes, _, _ := r.Run(context.Background(), tasks, 1, false)
	if outcomes[0].Status != result.Errored {
		t.Errorf("status = %q, want error", outcomes[0].Status)
	}
}

// TestRunQueuesConflicts exercises the core safety behavior: when two tasks both
// edit the same line of the same file, the first cherry-picks cleanly (merged)
// and the second conflicts, producing a QueuedConflict outcome with a non-empty
// branch so the user can resolve it manually.
func TestRunQueuesConflicts(t *testing.T) {
	repo, _ := vcs.Open(initRepo(t))
	// Both tasks overwrite seed.txt (already exists from initRepo) with different
	// content on the same line, guaranteeing a conflict on the second cherry-pick.
	tasks := []task.Task{
		{ID: "x", Prompt: "edit seed x", Agent: "fake", Gate: "true"},
		{ID: "y", Prompt: "edit seed y", Agent: "fake", Gate: "true"},
	}
	bodies := map[string]string{"x": "from-x\n", "y": "from-y\n"}
	r := Runner{
		Repo: repo, WorkRoot: t.TempDir(),
		Reporter: NewTextReporter(os.Stderr),
		Now:      func() string { return "ts" },
		AgentFor: func(tk task.Task) (agent.Agent, error) {
			return fakeAgent{name: "fake", file: "seed.txt", body: bodies[tk.ID]}, nil
		},
	}
	// concurrency 1: task x runs first and merges; task y conflicts.
	outcomes, _, err := r.Run(context.Background(), tasks, 1, false)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(outcomes) != 2 {
		t.Fatalf("expected 2 outcomes, got %d", len(outcomes))
	}
	statuses := map[string]result.Status{}
	branches := map[string]string{}
	for _, o := range outcomes {
		statuses[o.Task.ID] = o.Status
		branches[o.Task.ID] = o.Branch
	}
	if statuses["x"] != result.Merged {
		t.Errorf("task x: status = %q, want merged", statuses["x"])
	}
	if statuses["y"] != result.QueuedConflict {
		t.Errorf("task y: status = %q, want queued: conflict", statuses["y"])
	}
	if branches["y"] == "" {
		t.Error("conflicted task y should keep a branch")
	}
}

func TestRunFinalGateResult(t *testing.T) {
	repo, _ := vcs.Open(initRepo(t))
	tasks := []task.Task{{ID: "a", Prompt: "p", Agent: "fake", Gate: "true"}}
	r := Runner{
		Repo: repo, WorkRoot: t.TempDir(),
		Reporter:  NewTextReporter(os.Stderr),
		Now:       func() string { return "ts" },
		FinalGate: "test -f a.txt", // passes only if the merge landed a.txt
		AgentFor: func(tk task.Task) (agent.Agent, error) {
			return fakeAgent{name: "fake", file: "a.txt", body: "a\n"}, nil
		},
	}
	_, _, fg, err := r.RunWithFinalGate(context.Background(), tasks, 1)
	if err != nil {
		t.Fatalf("RunWithFinalGate: %v", err)
	}
	if !fg.Passed {
		t.Errorf("final gate should pass; output=%q", fg.Output)
	}
}
