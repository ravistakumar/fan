package run

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ravistakumar/fan/internal/result"
	"github.com/ravistakumar/fan/internal/task"
)

func TestTextReporterEmitsStartAndFinish(t *testing.T) {
	var buf bytes.Buffer
	r := NewTextReporter(&buf)
	r.Start(task.Task{ID: "alpha"})
	r.Finish(result.Outcome{Task: task.Task{ID: "alpha"}, Status: result.Merged})

	out := buf.String()
	if !strings.Contains(out, "alpha") {
		t.Errorf("reporter output missing task id:\n%s", out)
	}
	if !strings.Contains(out, string(result.Merged)) {
		t.Errorf("reporter output missing status:\n%s", out)
	}
}
