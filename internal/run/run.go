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
	// FinalGate, when non-empty, is run once on the integration branch after all
	// merges to catch interaction bugs between independently-passing tasks.
	FinalGate string
}

// candidate is the intermediate result of the parallel phase. It carries the
// facts gathered while running a task; the terminal Status is derived from them
// by result.Decide. CherryClean is filled in during the serial phase.
type candidate struct {
	task    task.Task
	sha     string // commit sha when the agent produced changes (empty otherwise)
	wtDir   string // worktree to clean up ("" when none was created)
	dec     result.Decision
	detail  string          // error message or gate output for the eventual Outcome
	outcome *result.Outcome // set in the parallel phase for tasks decided without a cherry-pick
}

// pendingMerge reports whether the task ran cleanly and passed its gate, so its
// final status depends on the serial cherry-pick. Everything else is already
// decidable from the facts gathered in runTask.
func (c candidate) pendingMerge() bool {
	return !c.dec.AgentErr && c.dec.Changed && c.dec.GatePassed
}

// Run executes tasks against a fresh integration branch and returns the
// outcomes and branch name. It does not run a final gate; use RunWithFinalGate
// for that.
func (r Runner) Run(ctx context.Context, tasks []task.Task, concurrency int, _ bool) ([]result.Outcome, string, error) {
	base, err := r.Repo.Head()
	if err != nil {
		return nil, "", err
	}
	branch := "fan/" + r.now()
	intDir := filepath.Join(r.WorkRoot, "integration")
	if err := r.Repo.CreateWorktreeBranch(intDir, branch, base); err != nil {
		return nil, "", fmt.Errorf("create integration worktree: %w", err)
	}
	defer func() { _ = r.Repo.RemoveWorktree(intDir) }()
	return r.execute(ctx, tasks, concurrency, base, intDir), branch, nil
}

// RunWithFinalGate runs the pipeline, then runs FinalGate on the integration
// branch (if set) before tearing the integration worktree down.
func (r Runner) RunWithFinalGate(ctx context.Context, tasks []task.Task, concurrency int) ([]result.Outcome, string, gate.Result, error) {
	base, err := r.Repo.Head()
	if err != nil {
		return nil, "", gate.Result{}, err
	}
	branch := "fan/" + r.now()
	intDir := filepath.Join(r.WorkRoot, "integration")
	if err := r.Repo.CreateWorktreeBranch(intDir, branch, base); err != nil {
		return nil, "", gate.Result{}, fmt.Errorf("create integration worktree: %w", err)
	}
	defer func() { _ = r.Repo.RemoveWorktree(intDir) }()

	outcomes := r.execute(ctx, tasks, concurrency, base, intDir)

	fg := gate.Result{Passed: true}
	if r.FinalGate != "" {
		fg = gate.Run(ctx, r.FinalGate, intDir)
	}
	return outcomes, branch, fg, nil
}

// execute runs the parallel phase (worktree + agent + gate per task) and the
// serial phase (cherry-pick onto intDir), returning the outcome for each task.
func (r Runner) execute(ctx context.Context, tasks []task.Task, concurrency int, base, intDir string) []result.Outcome {
	// Parallel phase: worktree + agent + gate for each task. Tasks that don't
	// reach the cherry-pick (error / no-op / gate-fail) are fully decided here
	// and reported as they finish, keeping progress live.
	cands := schedule.Run(ctx, tasks, concurrency, func(ctx context.Context, _ int, tk task.Task) candidate {
		r.Reporter.Start(tk)
		c := r.runTask(ctx, tk, base)
		if !c.pendingMerge() {
			c.outcome = ptr(r.outcomeFor(c))
			r.Reporter.Finish(*c.outcome)
		}
		return c
	})

	// Serial phase: cherry-pick gate-passing candidates one at a time, then let
	// result.Decide turn the cherry-pick result into the final status.
	outcomes := make([]result.Outcome, len(cands))
	for i, c := range cands {
		if c.outcome != nil {
			outcomes[i] = *c.outcome
			r.removeWorktree(c.wtDir)
			continue
		}
		if clean, err := r.Repo.CherryPick(intDir, c.sha); err != nil {
			c.dec.AgentErr = true // a cherry-pick that errored outright is a failed run
			c.detail = err.Error()
		} else {
			c.dec.CherryClean = clean
		}
		outcomes[i] = r.outcomeFor(c)
		r.Reporter.Finish(outcomes[i])
		r.removeWorktree(c.wtDir)
	}
	return outcomes
}

// outcomeFor derives a task's terminal Outcome from its gathered facts via
// result.Decide, and keeps a branch for any queued (conflict / gate-fail) task
// so its work stays reviewable.
func (r Runner) outcomeFor(c candidate) result.Outcome {
	status := result.Decide(c.dec)
	branch := ""
	if status == result.QueuedConflict || status == result.QueuedGateFail {
		branch = "fan/" + c.task.ID
		_ = r.Repo.CreateBranch(branch, c.sha)
	}
	return result.Outcome{Task: c.task, Status: status, Branch: branch, Detail: c.detail}
}

// runTask creates a worktree, runs the agent, commits, and runs the gate. It
// returns a candidate carrying the facts (agent error, changes, gate result)
// from which result.Decide derives the terminal status.
func (r Runner) runTask(ctx context.Context, tk task.Task, base string) candidate {
	wtDir := filepath.Join(r.WorkRoot, "wt", tk.ID)

	ag, err := r.agentFor(tk)
	if err != nil {
		return candidate{task: tk, dec: result.Decision{AgentErr: true}, detail: err.Error()}
	}
	if err := r.Repo.AddWorktree(wtDir, base); err != nil {
		return candidate{task: tk, dec: result.Decision{AgentErr: true}, detail: err.Error()}
	}

	if _, err := ag.Run(ctx, tk.Prompt, wtDir); err != nil {
		return candidate{task: tk, wtDir: wtDir, dec: result.Decision{AgentErr: true}, detail: err.Error()}
	}

	sha, changed, err := r.Repo.CommitAll(wtDir, fmt.Sprintf("fan(%s): %s", tk.ID, tk.Prompt))
	if err != nil {
		return candidate{task: tk, wtDir: wtDir, dec: result.Decision{AgentErr: true}, detail: err.Error()}
	}
	if !changed {
		return candidate{task: tk, wtDir: wtDir, dec: result.Decision{Changed: false}}
	}

	g := gate.Run(ctx, tk.Gate, wtDir)
	if !g.Passed {
		return candidate{
			task: tk, sha: sha, wtDir: wtDir,
			dec:    result.Decision{Changed: true, GatePassed: false},
			detail: gateDetail(tk.Gate, g.Output),
		}
	}

	return candidate{task: tk, sha: sha, wtDir: wtDir, dec: result.Decision{Changed: true, GatePassed: true}}
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
		_ = r.Repo.RemoveWorktree(dir)
	}
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
