package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/ravistakumar/fan/internal/result"
)

// NewModelForTest builds a model for runtime tests. It is exported for tests in
// the external tui_test package; production code uses the reporter, not the
// model, directly.
func NewModelForTest(total, concurrency int) tea.Model { return newModel(total, concurrency, nil) }

// StartForTest and FinishForTest build the internal messages for tests.
func StartForTest(id, agent string) tea.Msg { return startMsg{id: id, agent: agent} }
func FinishForTest(o result.Outcome) tea.Msg { return finishMsg{outcome: o} }
func QuitForTest() tea.Msg                   { return tea.Quit() }
