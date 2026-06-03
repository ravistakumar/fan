package result

import (
	"testing"

	"github.com/ravistakumar/fan/internal/task"
)

func TestDecide(t *testing.T) {
	cases := []struct {
		name string
		in   Decision
		want Status
	}{
		{"agent error wins", Decision{AgentErr: true, Changed: true, GatePassed: true, CherryClean: true}, Errored},
		{"no changes", Decision{Changed: false}, NoOp},
		{"gate fail", Decision{Changed: true, GatePassed: false}, QueuedGateFail},
		{"conflict", Decision{Changed: true, GatePassed: true, CherryClean: false}, QueuedConflict},
		{"merged", Decision{Changed: true, GatePassed: true, CherryClean: true}, Merged},
	}
	for _, c := range cases {
		if got := Decide(c.in); got != c.want {
			t.Errorf("%s: Decide = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestOutcomeHoldsTask(t *testing.T) {
	o := Outcome{Task: task.Task{ID: "x"}, Status: Merged}
	if o.Task.ID != "x" {
		t.Errorf("task not held: %+v", o)
	}
}
