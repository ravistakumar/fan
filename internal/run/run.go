package run

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/ravistakumar/fan/internal/agent"
	"github.com/ravistakumar/fan/internal/gate"
	"github.com/ravistakumar/fan/internal/result"
	"github.com/ravistakumar/fan/internal/schedule"
	"github.com/ravistakumar/fan/internal/task"
	"github.com/ravistakumar/fan/internal/vcs"
)

// Runner wires the fan pipeline: schedule tasks into isolated worktrees, run an
// agent + gate in each, then serially cherry-pick the gate-passing ones onto a
// throwaway integration branch.
type Runner struct {
	Repo     *vcs.Repo
	WorkRoot string // parent dir for .fan worktrees (usually repo root + "/.fan")
	Reporter Reporter

	// AgentFor returns the agent for a task. Defaults to agent.New(task.Agent)
	// when nil. Injected in tests.
	AgentFor func(task.Task) (agent.Agent, error)
	// NewAgent is the simple factory used when AgentFor is nil.
	NewAgent func(string) (agent.Agent, error)
	// Now returns the timestamp slug used in the integration branch name.
	Now func() string
}

// candidate is the intermediate result of the parallel phase: either a
// gate-passing commit ready to cherry-pick, or an already-terminal outcome.
type candidate struct {
	task     task.Task
	sha      string
	wtDir    string
	terminal *result.Outcome // non-nil if the task is already done (error/gate-fail/no-op)
}

// Run executes tasks and returns their outcomes plus the integration branch
// name. finalGate, when true, runs the first task's-style gate once more on the
// integration branch after all merges; its result is reported by the caller via
// the returned outcomes' overall state (see summary).
func (r Runner) Run(ctx context.Context, tasks []task.Task, concurrency int, finalGate bool) ([]result.Outcome, string, error) {
	base, err := r.Repo.Head()
	if err != nil {
		return nil, "", err
	}
	branch := "fan/" + r.now()

	intDir := filepath.Join(r.WorkRoot, "integration")
	if err := r.Repo.CreateWorktreeBranch(intDir, branch, base); err != nil {
		return nil, "", fmt.Errorf("create integration worktree: %w", err)
	}
	defer r.Repo.RemoveWorktree(intDir)

	// Parallel phase: worktree + agent + gate for each task.
	cands := schedule.Run(ctx, tasks, concurrency, func(ctx context.Context, _ int, tk task.Task) candidate {
		r.Reporter.Start(tk)
		c := r.runTask(ctx, tk, base)
		if c.terminal != nil {
			r.Reporter.Finish(*c.terminal)
		}
		return c
	})

	// Serial phase: cherry-pick gate-passing candidates one at a time.
	outcomes := make([]result.Outcome, len(cands))
	for i, c := range cands {
		if c.terminal != nil {
			outcomes[i] = *c.terminal
			r.removeWorktree(c.wtDir)
			continue
		}
		clean, err := r.Repo.CherryPick(intDir, c.sha)
		if err != nil {
			outcomes[i] = result.Outcome{Task: c.task, Status: result.Errored, Detail: err.Error()}
		} else if clean {
			outcomes[i] = result.Outcome{Task: c.task, Status: result.Merged}
		} else {
			kept := "fan/" + c.task.ID
			_ = r.Repo.CreateBranch(kept, c.sha)
			outcomes[i] = result.Outcome{Task: c.task, Status: result.QueuedConflict, Branch: kept}
		}
		r.Reporter.Finish(outcomes[i])
		r.removeWorktree(c.wtDir)
	}

	return outcomes, branch, nil
}

// runTask creates a worktree, runs the agent, commits, and runs the gate. It
// returns a candidate ready for cherry-pick, or a terminal outcome.
func (r Runner) runTask(ctx context.Context, tk task.Task, base string) candidate {
	wtDir := filepath.Join(r.WorkRoot, "wt", tk.ID)

	ag, err := r.agentFor(tk)
	if err != nil {
		return terminal(tk, result.Errored, "", err.Error())
	}
	if err := r.Repo.AddWorktree(wtDir, base); err != nil {
		return terminal(tk, result.Errored, "", err.Error())
	}

	if _, err := ag.Run(ctx, tk.Prompt, wtDir); err != nil {
		return candidate{task: tk, wtDir: wtDir, terminal: ptr(result.Outcome{Task: tk, Status: result.Errored, Detail: err.Error()})}
	}

	sha, changed, err := r.Repo.CommitAll(wtDir, fmt.Sprintf("fan(%s): %s", tk.ID, tk.Prompt))
	if err != nil {
		return candidate{task: tk, wtDir: wtDir, terminal: ptr(result.Outcome{Task: tk, Status: result.Errored, Detail: err.Error()})}
	}
	if !changed {
		return candidate{task: tk, wtDir: wtDir, terminal: ptr(result.Outcome{Task: tk, Status: result.NoOp})}
	}

	g := gate.Run(ctx, tk.Gate, wtDir)
	if !g.Passed {
		kept := "fan/" + tk.ID
		_ = r.Repo.CreateBranch(kept, sha)
		return candidate{task: tk, wtDir: wtDir, terminal: ptr(result.Outcome{
			Task: tk, Status: result.QueuedGateFail, Branch: kept, Detail: gateDetail(tk.Gate, g.Output)})}
	}

	return candidate{task: tk, sha: sha, wtDir: wtDir}
}

func (r Runner) agentFor(tk task.Task) (agent.Agent, error) {
	if r.AgentFor != nil {
		return r.AgentFor(tk)
	}
	if r.NewAgent != nil {
		return r.NewAgent(tk.Agent)
	}
	return agent.New(tk.Agent)
}

func (r Runner) now() string {
	if r.Now != nil {
		return r.Now()
	}
	return "run"
}

func (r Runner) removeWorktree(dir string) {
	if dir != "" {
		r.Repo.RemoveWorktree(dir)
	}
}

func terminal(tk task.Task, s result.Status, branch, detail string) candidate {
	return candidate{task: tk, terminal: ptr(result.Outcome{Task: tk, Status: s, Branch: branch, Detail: detail})}
}

func ptr(o result.Outcome) *result.Outcome { return &o }

func gateDetail(command, output string) string {
	if len(output) > 80 {
		output = output[:80] + "…"
	}
	if output == "" {
		return command + " failed"
	}
	return output
}
