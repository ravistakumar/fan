// Package result defines the terminal status of a fanned-out task and the pure
// decision that maps an execution's facts to that status.
package result

import "github.com/ravistakumar/fan/internal/task"

// Status is the terminal state of one task.
type Status string

const (
	Merged         Status = "merged"
	QueuedConflict Status = "queued: conflict"
	QueuedGateFail Status = "queued: gate-fail"
	Errored        Status = "error"
	NoOp           Status = "no-op"
)

// Outcome is the full record of one task's run.
type Outcome struct {
	Task   task.Task
	Status Status
	Branch string // kept branch for queued items (empty otherwise)
	Detail string // gate output snippet or error message
}

// Decision holds the facts gathered while running a task, in pipeline order:
// the agent ran (or errored), it changed files (or not), the gate passed (only
// evaluated when changed), and the cherry-pick was clean (only evaluated when
// the gate passed).
type Decision struct {
	AgentErr    bool
	Changed     bool
	GatePassed  bool
	CherryClean bool
}

// Decide maps a Decision to a Status. The order mirrors the run pipeline: an
// agent error short-circuits; no changes is a no-op; the gate is checked before
// any merge; a clean cherry-pick is a merge.
func Decide(d Decision) Status {
	switch {
	case d.AgentErr:
		return Errored
	case !d.Changed:
		return NoOp
	case !d.GatePassed:
		return QueuedGateFail
	case !d.CherryClean:
		return QueuedConflict
	default:
		return Merged
	}
}
