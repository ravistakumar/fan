package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ravistakumar/fan/internal/agent"
	"github.com/ravistakumar/fan/internal/planner"
	"github.com/ravistakumar/fan/internal/task"
)

func newPlanCmd() *cobra.Command {
	var (
		agentName string
		gateCmd   string
		save      string
		conc      int
		yes       bool
	)
	cmd := &cobra.Command{
		Use:   "plan <goal>",
		Short: "Have an agent draft a parallel task list you approve, then run it",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			goal := strings.Join(args, " ")
			if agentName == "" {
				return fmt.Errorf("--agent is required for planning")
			}
			ag, err := agent.New(agentName)
			if err != nil {
				return err
			}
			if !ag.Available() {
				return fmt.Errorf("agent %q is not installed or on PATH", agentName)
			}

			reply, err := ag.Run(context.Background(), planner.BuildMetaPrompt(goal), ".")
			if err != nil {
				return fmt.Errorf("planner agent failed: %w", err)
			}
			tasks, err := planner.ParsePlan(reply, agent.IsSupported, agentName, gateCmd)
			if err != nil {
				return err
			}

			fmt.Fprint(cmd.OutOrStdout(), renderPlanTable(tasks))

			if save != "" {
				if err := os.WriteFile(save, []byte(planToTOML(tasks, conc)), 0o644); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "\nplan saved to %s\n", save)
			}
			if !yes {
				fmt.Fprintln(cmd.OutOrStdout(), "\nRe-run with --yes to execute, or edit the saved file and `fan run` it.")
				return nil
			}
			// Execute immediately when approved with --yes.
			return runTasks(cmd, tasks, concOrDefault(conc), firstGate(tasks))
		},
	}
	cmd.Flags().StringVar(&agentName, "agent", "", "agent that drafts the plan and runs tasks")
	cmd.Flags().StringVar(&gateCmd, "gate", "", "default gate command for planned tasks")
	cmd.Flags().StringVar(&save, "save", "", "write the approved plan to this TOML file")
	cmd.Flags().IntVar(&conc, "concurrency", 0, "max concurrent agents (0 = cores-1)")
	cmd.Flags().BoolVar(&yes, "yes", false, "execute the plan without a second confirmation")
	return cmd
}

func renderPlanTable(tasks []task.Task) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-3s %-16s %-9s %s\n", "#", "id", "agent", "prompt")
	for i, t := range tasks {
		p := t.Prompt
		if len(p) > 50 {
			p = p[:50] + "…"
		}
		fmt.Fprintf(&b, "%-3d %-16s %-9s %s\n", i+1, t.ID, t.Agent, p)
	}
	return b.String()
}

func planToTOML(tasks []task.Task, conc int) string {
	var b strings.Builder
	b.WriteString("[defaults]\n")
	if conc > 0 {
		fmt.Fprintf(&b, "concurrency = %d\n", conc)
	}
	for _, t := range tasks {
		b.WriteString("\n[[task]]\n")
		fmt.Fprintf(&b, "id     = %q\n", t.ID)
		fmt.Fprintf(&b, "prompt = %q\n", t.Prompt)
		fmt.Fprintf(&b, "agent  = %q\n", t.Agent)
		if t.Gate != "" {
			fmt.Fprintf(&b, "gate   = %q\n", t.Gate)
		}
	}
	return b.String()
}
