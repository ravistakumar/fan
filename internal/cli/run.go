package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ravistakumar/fan/internal/agent"
	"github.com/ravistakumar/fan/internal/gate"
	"github.com/ravistakumar/fan/internal/result"
	"github.com/ravistakumar/fan/internal/run"
	"github.com/ravistakumar/fan/internal/summary"
	"github.com/ravistakumar/fan/internal/task"
	"github.com/ravistakumar/fan/internal/vcs"
)

func newRunCmd() *cobra.Command {
	var (
		each        string
		agentName   string
		gateCmd     string
		concurrency int
		only        string
		noFinalGate bool
	)
	cmd := &cobra.Command{
		Use:   "run [taskfile]",
		Short: "Fan tasks out in parallel, isolating and gating each",
		Args:  cobra.MaximumNArgs(2), // taskfile OR (--each + template)
		RunE: func(cmd *cobra.Command, args []string) error {
			tasks, conc, err := buildTasks(args, each, agentName, gateCmd, concurrency)
			if err != nil {
				return err
			}
			tasks = filterTasks(tasks, only)
			if len(tasks) == 0 {
				return fmt.Errorf("no tasks to run")
			}

			finalGate := ""
			if !noFinalGate {
				finalGate = firstGate(tasks)
			}
			return runTasks(cmd, tasks, conc, finalGate)
		},
	}
	cmd.Flags().StringVar(&each, "each", "", "fan a template over a glob or comma list (use {} placeholder)")
	cmd.Flags().StringVar(&agentName, "agent", "", "agent to use (claude|codex|opencode|aider|grok)")
	cmd.Flags().StringVar(&gateCmd, "gate", "", "gate command run in each worktree (empty = clean-apply-only)")
	cmd.Flags().IntVar(&concurrency, "concurrency", 0, "max concurrent agents (0 = cores-1)")
	cmd.Flags().StringVar(&only, "only", "", "run only these task ids (comma list)")
	cmd.Flags().BoolVar(&noFinalGate, "no-final-gate", false, "skip the gate run on the integration branch")
	return cmd
}

// buildTasks produces the task list and concurrency from either a task file
// (args[0]) or an inline --each template (args[0] is the template).
func buildTasks(args []string, each, agentName, gateCmd string, concurrency int) ([]task.Task, int, error) {
	if each != "" {
		if len(args) != 1 {
			return nil, 0, fmt.Errorf("--each requires exactly one template argument")
		}
		items, err := resolveEachItems(each)
		if err != nil {
			return nil, 0, err
		}
		tasks, err := task.Expand(items, args[0], agentName, gateCmd)
		if err != nil {
			return nil, 0, err
		}
		return tasks, concOrDefault(concurrency), nil
	}

	if len(args) != 1 {
		return nil, 0, fmt.Errorf("provide a task file, or use --each with a template")
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		return nil, 0, err
	}
	f, err := task.Parse(data, agent.IsSupported)
	if err != nil {
		return nil, 0, err
	}
	conc := f.Defaults.Concurrency
	if concurrency > 0 {
		conc = concurrency
	}
	// CLI flags override file defaults where set.
	for i := range f.Tasks {
		if agentName != "" {
			f.Tasks[i].Agent = agentName
		}
	}
	return f.Tasks, conc, nil
}

// resolveEachItems turns a --each value into a list of items: a filesystem glob
// when it contains glob metacharacters, otherwise a comma-separated list.
func resolveEachItems(each string) ([]string, error) {
	each = strings.TrimSpace(each)
	if each == "" {
		return nil, fmt.Errorf("--each is empty")
	}
	if strings.ContainsAny(each, "*?[") {
		matches, err := filepath.Glob(each)
		if err != nil {
			return nil, err
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("glob %q matched no files", each)
		}
		return matches, nil
	}
	parts := strings.Split(each, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("--each has no items")
	}
	return out, nil
}

func filterTasks(tasks []task.Task, only string) []task.Task {
	keep := idSet(only)
	if keep == nil {
		return tasks
	}
	out := tasks[:0:0]
	for _, t := range tasks {
		if keep[t.ID] {
			out = append(out, t)
		}
	}
	return out
}

// filterIDs is the string-slice counterpart used in tests.
func filterIDs(ids []string, only string) []string {
	keep := idSet(only)
	if keep == nil {
		return ids
	}
	out := []string{}
	for _, id := range ids {
		if keep[id] {
			out = append(out, id)
		}
	}
	return out
}

func idSet(only string) map[string]bool {
	only = strings.TrimSpace(only)
	if only == "" {
		return nil
	}
	m := map[string]bool{}
	for _, p := range strings.Split(only, ",") {
		if p = strings.TrimSpace(p); p != "" {
			m[p] = true
		}
	}
	return m
}

func firstGate(tasks []task.Task) string {
	for _, t := range tasks {
		if t.Gate != "" {
			return t.Gate
		}
	}
	return ""
}

func concOrDefault(c int) int {
	if c > 0 {
		return c
	}
	return task.DefaultConcurrency()
}

// runTasks opens the repo, runs the tasks, and prints the summary. Shared by
// `fan run` and `fan plan --yes`. finalGate is the command to run on the
// integration branch after merges ("" to skip it).
func runTasks(cmd *cobra.Command, tasks []task.Task, conc int, finalGate string) error {
	repo, err := vcs.Open(".")
	if err != nil {
		return err
	}
	workRoot := filepath.Join(repo.Root(), ".fan")
	if err := os.MkdirAll(workRoot, 0o755); err != nil {
		return err
	}
	r := run.Runner{
		Repo:      repo,
		WorkRoot:  workRoot,
		Reporter:  run.NewTextReporter(cmd.ErrOrStderr()),
		FinalGate: finalGate,
		Now:       timestamp,
	}
	outcomes, branch, fg, err := r.RunWithFinalGate(context.Background(), tasks, conc)
	if err != nil {
		return err
	}
	fmt.Fprint(cmd.OutOrStdout(), summary.Render(outcomes, branch, fg))
	return runStatusErr(outcomes, fg)
}

// runStatusErr returns a non-nil error when a run should be treated as failed:
// the final integration gate failed, or every task ended in an error. Queued
// conflicts and gate-failures are expected partial outcomes, not tool failures,
// so they do not trigger a non-zero exit.
func runStatusErr(outcomes []result.Outcome, fg gate.Result) error {
	if !fg.Passed {
		return fmt.Errorf("integration gate failed")
	}
	if len(outcomes) > 0 {
		allErrored := true
		for _, o := range outcomes {
			if o.Status != result.Errored {
				allErrored = false
				break
			}
		}
		if allErrored {
			return fmt.Errorf("all %d tasks failed", len(outcomes))
		}
	}
	return nil
}
