package tui_test

import (
	"io"
	"testing"
	"time"

	teatest "github.com/charmbracelet/x/exp/teatest"

	"github.com/ravistakumar/fan/internal/result"
	"github.com/ravistakumar/fan/internal/run"
	"github.com/ravistakumar/fan/internal/task"
	"github.com/ravistakumar/fan/internal/tui"
)

// Compile-time check that the adapter satisfies the run.Reporter seam.
var _ run.Reporter = (*tui.Reporter)(nil)

func TestReporterConstructs(t *testing.T) {
	r := tui.New(nil, func() {})
	if r == nil {
		t.Fatal("New returned nil")
	}
}

// TestModelRendersThroughRuntime drives the model through the real bubbletea
// runtime via teatest, exercising Init/tick/Update/View end to end.
func TestModelRendersThroughRuntime(t *testing.T) {
	tm := teatest.NewTestModel(t, tui.NewModelForTest(3, 4))
	tm.Send(tui.StartForTest("modal", "claude"))
	tm.Send(tui.FinishForTest(result.Outcome{Task: task.Task{ID: "button"}, Status: result.Merged}))
	tm.Send(tui.QuitForTest())

	out, err := io.ReadAll(tm.FinalOutput(t, teatest.WithFinalTimeout(2*time.Second)))
	if err != nil {
		t.Fatalf("read final output: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("expected rendered output from the runtime")
	}
}
