package cli

import (
	"testing"

	"github.com/ravistakumar/fan/internal/gate"
	"github.com/ravistakumar/fan/internal/result"
	"github.com/ravistakumar/fan/internal/task"
)

func outcome(s result.Status) result.Outcome {
	return result.Outcome{Task: task.Task{}, Status: s}
}

func TestRunStatusErrGateFailed(t *testing.T) {
	outcomes := []result.Outcome{outcome(result.Merged)}
	fg := gate.Result{Passed: false, Output: "gate output"}
	if err := runStatusErr(outcomes, fg); err == nil {
		t.Fatal("expected non-nil error when gate failed")
	}
}

func TestRunStatusErrAllErrored(t *testing.T) {
	outcomes := []result.Outcome{outcome(result.Errored), outcome(result.Errored)}
	fg := gate.Result{Passed: true}
	if err := runStatusErr(outcomes, fg); err == nil {
		t.Fatal("expected non-nil error when all tasks errored")
	}
}

func TestRunStatusErrMixedOutcomes(t *testing.T) {
	outcomes := []result.Outcome{outcome(result.Merged), outcome(result.QueuedConflict)}
	fg := gate.Result{Passed: true}
	if err := runStatusErr(outcomes, fg); err != nil {
		t.Fatalf("expected nil error for mixed outcomes, got: %v", err)
	}
}

func TestRunStatusErrEmptyOutcomesGatePassed(t *testing.T) {
	fg := gate.Result{Passed: true}
	if err := runStatusErr(nil, fg); err != nil {
		t.Fatalf("expected nil error for empty outcomes with gate passed, got: %v", err)
	}
}
