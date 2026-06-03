package task

import "testing"

func TestExpandSubstitutesAndDerivesIDs(t *testing.T) {
	items := []string{"src/ui/Button.tsx", "src/ui/Modal.tsx"}
	tasks, err := Expand(items, "Migrate {} to the new API", "codex", "npm test")
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("tasks = %d, want 2", len(tasks))
	}
	if tasks[0].Prompt != "Migrate src/ui/Button.tsx to the new API" {
		t.Errorf("prompt = %q", tasks[0].Prompt)
	}
	if tasks[0].ID != "button" || tasks[1].ID != "modal" {
		t.Errorf("ids = %q,%q want button,modal", tasks[0].ID, tasks[1].ID)
	}
	if tasks[0].Agent != "codex" || tasks[0].Gate != "npm test" {
		t.Errorf("agent/gate not applied: %+v", tasks[0])
	}
}

func TestExpandDeduplicatesIDs(t *testing.T) {
	items := []string{"a/Button.tsx", "b/Button.tsx"}
	tasks, err := Expand(items, "do {}", "codex", "")
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if tasks[0].ID == tasks[1].ID {
		t.Errorf("ids not unique: %q == %q", tasks[0].ID, tasks[1].ID)
	}
}

func TestExpandRequiresPlaceholder(t *testing.T) {
	if _, err := Expand([]string{"x"}, "no placeholder here", "codex", ""); err == nil {
		t.Fatal("expected error for missing {} placeholder")
	}
}

func TestExpandRejectsEmptyItems(t *testing.T) {
	if _, err := Expand(nil, "do {}", "codex", ""); err == nil {
		t.Fatal("expected error for empty item list")
	}
}
