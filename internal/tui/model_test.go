package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ravistakumar/fan/internal/result"
	"github.com/ravistakumar/fan/internal/task"
)

// taskWithID builds a task.Task carrying just an ID, for outcome construction.
func taskWithID(id string) task.Task { return task.Task{ID: id} }

// step applies one message and returns the updated model.
func step(m model, msg tea.Msg) model {
	next, _ := m.Update(msg)
	return next.(model)
}

func TestModelShowsRunningTaskWithElapsed(t *testing.T) {
	t0 := time.Unix(1000, 0)
	m := newModel(4, 8, nil)
	m = step(m, tickMsg(t0))                       // now = t0
	m = step(m, startMsg{id: "modal", agent: "claude"})
	m = step(m, tickMsg(t0.Add(3200*time.Millisecond))) // now = t0 + 3.2s

	v := m.View()
	for _, want := range []string{"4 tasks", "up to 8", "running", "modal", "claude", "3.2s"} {
		if !strings.Contains(v, want) {
			t.Errorf("View missing %q\n---\n%s", want, v)
		}
	}
}

func TestModelMovesFinishedTaskToRecentAndCounts(t *testing.T) {
	t0 := time.Unix(1000, 0)
	m := newModel(2, 4, nil)
	m = step(m, tickMsg(t0))
	m = step(m, startMsg{id: "button", agent: "codex"})
	m = step(m, finishMsg{outcome: result.Outcome{
		Task: taskWithID("button"), Status: result.Merged}})

	v := m.View()
	if !strings.Contains(v, "recently done") || !strings.Contains(v, "button") {
		t.Errorf("View should list the finished task under recently done:\n%s", v)
	}
	if !strings.Contains(v, "1/2 done") || !strings.Contains(v, "1 merged") {
		t.Errorf("counter wrong:\n%s", v)
	}
	if m.done != 1 || m.counts[result.Merged] != 1 {
		t.Errorf("done=%d merged=%d, want 1 and 1", m.done, m.counts[result.Merged])
	}
}

func TestModelCancelCallsCancelAndQuits(t *testing.T) {
	called := false
	m := newModel(1, 1, func() { called = true })
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = next.(model)
	if !called {
		t.Error("ctrl+c should call the cancel func")
	}
	if !m.canceling {
		t.Error("ctrl+c should set canceling")
	}
	if cmd == nil {
		t.Error("ctrl+c should return a command (tea.Quit)")
	}
}

func TestModelTruncatesRecentlyDoneToHeight(t *testing.T) {
	m := newModel(20, 2, nil)
	m = step(m, tea.WindowSizeMsg{Width: 80, Height: 10})
	m = step(m, tickMsg(time.Unix(0, 0)))
	// finish 12 tasks; with height 10 only a few "recently done" rows can show.
	for i := 0; i < 12; i++ {
		id := "t" + string(rune('a'+i))
		m = step(m, finishMsg{outcome: result.Outcome{Task: taskWithID(id), Status: result.Merged}})
	}
	v := m.View()
	if strings.Count(v, "\n") > 12 {
		t.Errorf("view exceeded height budget:\n%s", v)
	}
	if !strings.Contains(v, "12/20 done") {
		t.Errorf("counter should still be accurate:\n%s", v)
	}
}
