// Package tui renders fan's live progress as a full-screen bubbletea view. It
// implements run.Reporter structurally without importing run, so bubbletea
// stays out of the core run package.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ravistakumar/fan/internal/result"
)

// Messages sent into the program by the reporter adapter and the tick loop.
type startMsg struct{ id, agent string }
type finishMsg struct{ outcome result.Outcome }
type tickMsg time.Time

const tickInterval = 100 * time.Millisecond

func tick() tea.Cmd {
	return tea.Tick(tickInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

var spinnerFrames = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

type taskState struct {
	id, agent string
	status    result.Status
	started   time.Time
}

type model struct {
	total, concurrency int
	cancel             context.CancelFunc
	now                time.Time
	frame              int
	order              []string // running ids, in start order
	running            map[string]*taskState
	recent             []taskState // completed, newest last
	counts             map[result.Status]int
	done               int
	width, height      int
	canceling          bool
}

func newModel(total, concurrency int, cancel context.CancelFunc) model {
	return model{
		total: total, concurrency: concurrency, cancel: cancel,
		running: map[string]*taskState{},
		counts:  map[result.Status]int{},
		width:   80, height: 24,
	}
}

func (m model) Init() tea.Cmd { return tick() }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.canceling = true
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		}
	case tickMsg:
		m.now = time.Time(msg)
		m.frame++
		return m, tick()
	case startMsg:
		m.running[msg.id] = &taskState{id: msg.id, agent: msg.agent, started: m.now}
		m.order = append(m.order, msg.id)
	case finishMsg:
		id := msg.outcome.Task.ID
		st := taskState{id: id, status: msg.outcome.Status}
		if cur, ok := m.running[id]; ok {
			st.agent, st.started = cur.agent, cur.started
			delete(m.running, id)
			m.order = removeID(m.order, id)
		}
		m.recent = append(m.recent, st)
		m.counts[msg.outcome.Status]++
		m.done++
	}
	return m, nil
}

func (m model) View() string {
	var b strings.Builder

	header := fmt.Sprintf("  fan · %d tasks · up to %d in parallel", m.total, m.concurrency)
	if m.canceling {
		header += "  (canceling…)"
	}
	b.WriteString(lipgloss.NewStyle().Bold(true).Render(header))
	b.WriteString("\n\n")

	b.WriteString("  running\n")
	spin := string(spinnerFrames[m.frame%len(spinnerFrames)])
	for _, id := range m.order {
		st := m.running[id]
		if st == nil {
			continue
		}
		fmt.Fprintf(&b, "    %s %-18s %-8s %s\n",
			spin, trunc(st.id, 18), st.agent, fmtDur(m.now.Sub(st.started)))
	}

	if rec := m.recentToShow(); len(rec) > 0 {
		b.WriteString("  recently done\n")
		for _, st := range rec {
			fmt.Fprintf(&b, "    %s %-18s %s\n",
				glyph(st.status), trunc(st.id, 18),
				statusStyle(st.status).Render(string(st.status)))
		}
	}

	b.WriteString("\n  ")
	b.WriteString(m.counterLine())
	b.WriteString("\n")
	return b.String()
}

// recentToShow returns the most recent completions that fit the remaining
// height after the header, running block, and counter.
func (m model) recentToShow() []taskState {
	// Fixed chrome: header(1) + blank(1) + "running"(1) + blank(1) + counter(1)
	// + "recently done"(1) = 6 lines, plus one line per running row.
	budget := m.height - 6 - len(m.running)
	if budget < 0 {
		budget = 0
	}
	if len(m.recent) <= budget {
		return m.recent
	}
	return m.recent[len(m.recent)-budget:]
}

func (m model) counterLine() string {
	return fmt.Sprintf("%d/%d done · %d merged · %d gate-fail · %d conflict",
		m.done, m.total,
		m.counts[result.Merged], m.counts[result.QueuedGateFail], m.counts[result.QueuedConflict])
}

func removeID(ids []string, id string) []string {
	out := make([]string, 0, len(ids))
	for _, x := range ids {
		if x != id {
			out = append(out, x)
		}
	}
	return out
}

func fmtDur(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func glyph(s result.Status) string {
	switch s {
	case result.Merged:
		return "✓"
	case result.QueuedConflict:
		return "⚠"
	case result.QueuedGateFail, result.Errored:
		return "✗"
	default:
		return "·"
	}
}

func statusStyle(s result.Status) lipgloss.Style {
	switch s {
	case result.Merged:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("2")) // green
	case result.QueuedConflict:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("3")) // yellow
	case result.QueuedGateFail, result.Errored:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("1")) // red
	default:
		return lipgloss.NewStyle().Faint(true)
	}
}
