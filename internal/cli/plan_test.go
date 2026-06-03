package cli

import (
	"testing"

	"github.com/ravistakumar/fan/internal/task"
)

func TestRenderPlanTable(t *testing.T) {
	tasks := []task.Task{
		{ID: "btn", Agent: "codex", Prompt: "migrate Button", Gate: "npm test"},
		{ID: "modal", Agent: "claude", Prompt: "migrate Modal", Gate: "npm test"},
	}
	out := renderPlanTable(tasks)
	for _, want := range []string{"btn", "modal", "codex", "claude", "migrate Button"} {
		if !contains(out, want) {
			t.Errorf("plan table missing %q:\n%s", want, out)
		}
	}
}

func TestPlanToTOMLRoundTrips(t *testing.T) {
	tasks := []task.Task{{ID: "a", Agent: "codex", Prompt: "do a", Gate: "go test ./..."}}
	data := planToTOML(tasks, 4)
	f, err := task.Parse([]byte(data), func(string) bool { return true })
	if err != nil {
		t.Fatalf("round-trip parse: %v\n%s", err, data)
	}
	if f.Tasks[0].ID != "a" || f.Tasks[0].Prompt != "do a" {
		t.Errorf("round-trip lost data: %+v", f.Tasks[0])
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && indexOf(s, sub) >= 0 }
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
