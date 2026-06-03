package planner

import (
	"strings"
	"testing"
)

func anyAgent(string) bool { return true }

func TestBuildMetaPromptIncludesGoalAndSchema(t *testing.T) {
	p := BuildMetaPrompt("migrate src/ui to the new API")
	if !strings.Contains(p, "migrate src/ui to the new API") {
		t.Error("meta prompt missing goal")
	}
	if !strings.Contains(p, "\"tasks\"") || !strings.Contains(p, "\"prompt\"") {
		t.Error("meta prompt missing JSON schema hints")
	}
}

func TestParsePlanExtractsTasks(t *testing.T) {
	reply := "Sure, here is the plan:\n```json\n" +
		`{"tasks":[{"id":"btn","prompt":"migrate Button"},{"id":"modal","prompt":"migrate Modal","agent":"claude"}]}` +
		"\n```\n"
	tasks, err := ParsePlan(reply, anyAgent, "codex", "npm test")
	if err != nil {
		t.Fatalf("ParsePlan: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("tasks = %d, want 2", len(tasks))
	}
	if tasks[0].Agent != "codex" || tasks[0].Gate != "npm test" {
		t.Errorf("defaults not applied: %+v", tasks[0])
	}
	if tasks[1].Agent != "claude" {
		t.Errorf("override not applied: %+v", tasks[1])
	}
}

func TestParsePlanRejectsUnknownAgent(t *testing.T) {
	reply := `{"tasks":[{"id":"x","prompt":"p","agent":"bogus"}]}`
	if _, err := ParsePlan(reply, func(string) bool { return false }, "codex", ""); err == nil {
		t.Fatal("expected unknown-agent error")
	}
}

func TestParsePlanRejectsNoTasks(t *testing.T) {
	if _, err := ParsePlan(`{"tasks":[]}`, anyAgent, "codex", ""); err == nil {
		t.Fatal("expected error for empty task list")
	}
}
