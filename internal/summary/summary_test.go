package summary

import (
	"strings"
	"testing"

	"github.com/ravistakumar/fan/internal/gate"
	"github.com/ravistakumar/fan/internal/result"
	"github.com/ravistakumar/fan/internal/task"
)

func TestRenderCountsAndNextSteps(t *testing.T) {
	outcomes := []result.Outcome{
		{Task: task.Task{ID: "a"}, Status: result.Merged},
		{Task: task.Task{ID: "b"}, Status: result.Merged},
		{Task: task.Task{ID: "c"}, Status: result.QueuedConflict, Branch: "fan/c"},
		{Task: task.Task{ID: "d"}, Status: result.QueuedGateFail, Branch: "fan/d", Detail: "npm test failed"},
	}
	out := Render(outcomes, "fan/2026-06-03-1432", gate.Result{Passed: true})

	for _, want := range []string{"2 merged", "1 conflict", "1 gate-fail", "fan/2026-06-03-1432", "git merge fan/2026-06-03-1432", "fan/c", "fan/d"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary missing %q\n---\n%s", want, out)
		}
	}
}

func TestRenderFinalGateFailIsFlagged(t *testing.T) {
	out := Render([]result.Outcome{{Task: task.Task{ID: "a"}, Status: result.Merged}},
		"fan/x", gate.Result{Passed: false, Output: "boom"})
	if !strings.Contains(strings.ToLower(out), "fail") {
		t.Errorf("final gate failure not flagged:\n%s", out)
	}
}
