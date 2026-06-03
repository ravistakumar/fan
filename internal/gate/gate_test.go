package gate

import (
	"context"
	"testing"
)

func TestEmptyGatePasses(t *testing.T) {
	r := Run(context.Background(), "", t.TempDir())
	if !r.Passed {
		t.Error("empty gate should pass (clean-apply-only)")
	}
}

func TestPassingCommand(t *testing.T) {
	r := Run(context.Background(), "true", t.TempDir())
	if !r.Passed {
		t.Errorf("`true` should pass; output=%q", r.Output)
	}
}

func TestFailingCommandCapturesOutput(t *testing.T) {
	r := Run(context.Background(), "echo nope; false", t.TempDir())
	if r.Passed {
		t.Error("`false` should fail")
	}
	if r.Output == "" {
		t.Error("expected captured output")
	}
}
