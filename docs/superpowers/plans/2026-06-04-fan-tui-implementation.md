# fan live TUI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an opt-in, full-screen live TUI progress view to `fan` — running tasks update in place (spinner, agent, elapsed), with rolled-up status counts — that falls back to the existing text reporter on non-TTY / `--plain` and supports graceful Ctrl-C cancellation.

**Architecture:** A second implementation of the existing `run.Reporter` seam. The `Reporter` interface gains `Begin`/`End` lifecycle methods (no-ops on the text reporter). A new `internal/tui` package holds a bubbletea-backed reporter and model; it implements `run.Reporter` structurally and never imports `run`, keeping bubbletea out of the core. The CLI selects TUI vs text by TTY detection and wires a cancellable context.

**Tech Stack:** Go 1.23, `github.com/charmbracelet/bubbletea`, `github.com/charmbracelet/lipgloss`, `golang.org/x/term` (TTY); test-only `github.com/charmbracelet/x/exp/teatest`.

**Spec:** `docs/superpowers/specs/2026-06-04-fan-tui-design.md`

---

## File Structure

```
internal/run/reporter.go   MODIFY: add Begin/End to Reporter; no-op them on TextReporter
internal/run/run.go        MODIFY: execute() calls Begin/End; runTask() gets a ctx guard
internal/run/run_test.go   MODIFY: Reporter spy test; canceled-ctx test
internal/tui/model.go      CREATE: bubbletea model (state, Init, Update, View) + helpers
internal/tui/model_test.go CREATE: unit tests over Update/View (package tui)
internal/tui/reporter.go   CREATE: tui.Reporter adapter owning the tea.Program
internal/tui/reporter_test.go CREATE: conformance assertion + teatest runtime test (package tui_test)
internal/tui/tty.go        CREATE: IsTerminal(*os.File) bool
internal/tui/tty_test.go   CREATE: IsTerminal fallback test
internal/cli/run.go        MODIFY: runTasks gains plain bool + ctx/cancel + reporter selection; --plain flag
```

---

## Task 1: Extend the Reporter seam with Begin/End

**Files:**
- Modify: `internal/run/reporter.go`
- Modify: `internal/run/run.go` (call Begin/End in `execute`)
- Test: `internal/run/run_test.go`

- [ ] **Step 1: Write the failing test (append to `internal/run/run_test.go`)**

```go
// spyReporter records the lifecycle calls the runner makes.
type spyReporter struct {
	begins, ends, starts, finishes int
	total, concurrency             int
}

func (s *spyReporter) Begin(total, concurrency int) {
	s.begins++
	s.total, s.concurrency = total, concurrency
}
func (s *spyReporter) Start(task.Task)       { s.starts++ }
func (s *spyReporter) Finish(result.Outcome) { s.finishes++ }
func (s *spyReporter) End()                  { s.ends++ }

func TestExecuteBracketsRunWithBeginEnd(t *testing.T) {
	repo, _ := vcs.Open(initRepo(t))
	tasks := []task.Task{
		{ID: "a", Prompt: "make a", Agent: "fake", Gate: "true"},
		{ID: "b", Prompt: "make b", Agent: "fake", Gate: "true"},
	}
	files := map[string]string{"a": "a.txt", "b": "b.txt"}
	spy := &spyReporter{}
	r := Runner{
		Repo: repo, WorkRoot: t.TempDir(),
		Reporter: spy,
		Now:      func() string { return "ts" },
		AgentFor: func(tk task.Task) (agent.Agent, error) {
			return fakeAgent{name: "fake", file: files[tk.ID], body: tk.ID + "\n"}, nil
		},
	}
	if _, _, err := r.Run(context.Background(), tasks, 2, false); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if spy.begins != 1 || spy.ends != 1 {
		t.Errorf("begins=%d ends=%d, want 1 and 1", spy.begins, spy.ends)
	}
	if spy.starts != 2 || spy.finishes != 2 {
		t.Errorf("starts=%d finishes=%d, want 2 and 2", spy.starts, spy.finishes)
	}
	if spy.total != 2 || spy.concurrency != 2 {
		t.Errorf("Begin got total=%d concurrency=%d, want 2 and 2", spy.total, spy.concurrency)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/run/ -run TestExecuteBracketsRunWithBeginEnd -v`
Expected: FAIL — `*spyReporter does not implement Reporter` is NOT the error yet (the interface has no Begin/End, so `spyReporter` has extra methods but the runner never calls them → `begins=0`). It fails on the assertion `begins=0, want 1`.

- [ ] **Step 3: Add Begin/End to the Reporter interface and TextReporter**

In `internal/run/reporter.go`, change the interface and add no-op methods:

```go
// Reporter receives progress events over the lifetime of a run. Implementations
// must be safe for concurrent use: Start/Finish are called from worker
// goroutines. Begin and End bracket the run and are called once each.
type Reporter interface {
	Begin(total, concurrency int)
	Start(t task.Task)
	Finish(o result.Outcome)
	End()
}
```

Add these two methods to `TextReporter` (keep the existing Start/Finish unchanged):

```go
// Begin and End are no-ops for the text reporter; its output is purely the
// per-event lines from Start/Finish.
func (r *TextReporter) Begin(total, concurrency int) {}
func (r *TextReporter) End()                         {}
```

- [ ] **Step 4: Call Begin/End in `execute`**

In `internal/run/run.go`, at the very start of `execute` (before the `schedule.Run` call) add:

```go
	r.Reporter.Begin(len(tasks), concurrency)
	defer r.Reporter.End()
```

So the top of `execute` reads:

```go
func (r Runner) execute(ctx context.Context, tasks []task.Task, concurrency int, base, intDir string) []result.Outcome {
	r.Reporter.Begin(len(tasks), concurrency)
	defer r.Reporter.End()

	// Parallel phase: worktree + agent + gate for each task. ...
	cands := schedule.Run(ctx, tasks, concurrency, func(ctx context.Context, _ int, tk task.Task) candidate {
```

- [ ] **Step 5: Run the test and the full run suite**

Run: `go test ./internal/run/ -v`
Expected: PASS — the new test plus all existing tests (`TextReporter` now has no-op Begin/End and still satisfies the interface).

- [ ] **Step 6: Commit**

```bash
git add internal/run/reporter.go internal/run/run.go internal/run/run_test.go
git commit -m "Add Begin/End lifecycle to the Reporter seam"
```

---

## Task 2: Cancel guard in runTask

**Files:**
- Modify: `internal/run/run.go` (`runTask`)
- Test: `internal/run/run_test.go`

- [ ] **Step 1: Write the failing test (append to `internal/run/run_test.go`)**

```go
func TestRunTaskShortCircuitsOnCanceledContext(t *testing.T) {
	repo, _ := vcs.Open(initRepo(t))
	r := Runner{
		Repo: repo, WorkRoot: t.TempDir(),
		Reporter: NewTextReporter(os.Stderr),
		Now:      func() string { return "ts" },
		AgentFor: func(tk task.Task) (agent.Agent, error) {
			t.Fatalf("agent should not run for a canceled task")
			return nil, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled before the task starts
	c := r.runTask(ctx, task.Task{ID: "x", Prompt: "p", Agent: "fake", Gate: "true"}, "HEAD")
	if c.dec.AgentErr != true {
		t.Errorf("canceled task should be an agent error, got %+v", c.dec)
	}
	if c.detail != "canceled" {
		t.Errorf("detail = %q, want canceled", c.detail)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/run/ -run TestRunTaskShortCircuitsOnCanceledContext -v`
Expected: FAIL — the agent factory is called (hits `t.Fatalf`) because there is no ctx guard yet.

- [ ] **Step 3: Add the guard at the top of `runTask`**

In `internal/run/run.go`, make the guard the first statement in `runTask`:

```go
func (r Runner) runTask(ctx context.Context, tk task.Task, base string) candidate {
	if ctx.Err() != nil {
		return candidate{task: tk, dec: result.Decision{AgentErr: true}, detail: "canceled"}
	}
	wtDir := filepath.Join(r.WorkRoot, "wt", tk.ID)
	// ... unchanged ...
```

- [ ] **Step 4: Run the test and the full suite**

Run: `go test ./internal/run/ -v`
Expected: PASS (new test + all existing).

- [ ] **Step 5: Commit**

```bash
git add internal/run/run.go internal/run/run_test.go
git commit -m "Short-circuit not-yet-started tasks on context cancellation"
```

---

## Task 3: The bubbletea model

**Files:**
- Create: `internal/tui/model.go`
- Test: `internal/tui/model_test.go`

- [ ] **Step 1: Add the dependencies**

Run:
```bash
go get github.com/charmbracelet/bubbletea@latest
go get github.com/charmbracelet/lipgloss@latest
go mod tidy
```
Expected: `go.mod` gains bubbletea + lipgloss (and their indirect deps).

- [ ] **Step 2: Write the failing test (`internal/tui/model_test.go`, package `tui`)**

```go
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

```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/tui/ -v`
Expected: FAIL — `undefined: newModel` / `model` / `startMsg`.

- [ ] **Step 4: Write the model (`internal/tui/model.go`)**

```go
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
		b.WriteString(fmt.Sprintf("    %s %-18s %-8s %s\n",
			spin, trunc(st.id, 18), st.agent, fmtDur(m.now.Sub(st.started))))
	}

	if rec := m.recentToShow(); len(rec) > 0 {
		b.WriteString("  recently done\n")
		for _, st := range rec {
			b.WriteString(fmt.Sprintf("    %s %-18s %s\n",
				glyph(st.status), trunc(st.id, 18),
				statusStyle(st.status).Render(string(st.status))))
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
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -v`
Expected: PASS (all four model tests).

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/tui/model.go internal/tui/model_test.go
git commit -m "Add bubbletea model for the live TUI"
```

---

## Task 4: The TUI Reporter adapter

**Files:**
- Create: `internal/tui/reporter.go`
- Test: `internal/tui/reporter_test.go`

- [ ] **Step 1: Add the test-only dependency**

Run:
```bash
go get github.com/charmbracelet/x/exp/teatest@latest
go mod tidy
```

- [ ] **Step 2: Write the failing test (`internal/tui/reporter_test.go`, package `tui_test`)**

```go
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
```

This test needs small test-only constructors exported from the `tui` package (so the external `tui_test` package can build a model and messages). Add them in Step 4.

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/tui/ -run TestReporter -v`
Expected: FAIL — `undefined: tui.New` / `tui.Reporter` / `tui.NewModelForTest`.

- [ ] **Step 4: Write the adapter (`internal/tui/reporter.go`)**

```go
package tui

import (
	"context"
	"io"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ravistakumar/fan/internal/result"
	"github.com/ravistakumar/fan/internal/task"
)

// Reporter is a run.Reporter that renders progress with bubbletea. The program
// runs in a goroutine; Start/Finish send messages to it (goroutine-safe), and
// End quits it and waits for the screen to be restored.
type Reporter struct {
	out     io.Writer
	cancel  context.CancelFunc
	program *tea.Program
	done    chan struct{}
	endOnce sync.Once
}

// New returns a TUI reporter that renders to out and calls cancel when the user
// interrupts the run.
func New(out io.Writer, cancel context.CancelFunc) *Reporter {
	return &Reporter{out: out, cancel: cancel, done: make(chan struct{})}
}

func (r *Reporter) Begin(total, concurrency int) {
	r.program = tea.NewProgram(
		newModel(total, concurrency, r.cancel),
		tea.WithAltScreen(),
		tea.WithOutput(r.out),
	)
	go func() {
		_, _ = r.program.Run()
		close(r.done)
	}()
}

func (r *Reporter) Start(t task.Task) {
	if r.program != nil {
		r.program.Send(startMsg{id: t.ID, agent: t.Agent})
	}
}

func (r *Reporter) Finish(o result.Outcome) {
	if r.program != nil {
		r.program.Send(finishMsg{outcome: o})
	}
}

// End quits the program and blocks until it has exited and restored the
// terminal, so the caller can safely print the summary afterward. It is
// idempotent: a second call (e.g. after a user cancel already quit the program)
// returns immediately.
func (r *Reporter) End() {
	r.endOnce.Do(func() {
		if r.program != nil {
			r.program.Quit()
			<-r.done
		} else {
			close(r.done)
		}
	})
}
```

- [ ] **Step 5: Add the test-only helpers (`internal/tui/export_test_helpers.go`)**

These let the external test build a model and messages without exporting internals to production callers. Create `internal/tui/testsupport.go`:

```go
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
func StartForTest(id, agent string) tea.Msg          { return startMsg{id: id, agent: agent} }
func FinishForTest(o result.Outcome) tea.Msg         { return finishMsg{outcome: o} }
func QuitForTest() tea.Msg                            { return tea.Quit() }
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -v`
Expected: PASS (model tests + reporter tests). The conformance `var _ run.Reporter = (*tui.Reporter)(nil)` compiles, proving the adapter satisfies the seam.

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum internal/tui/reporter.go internal/tui/reporter_test.go internal/tui/testsupport.go
git commit -m "Add bubbletea-backed TUI reporter adapter"
```

---

## Task 5: TTY detection

**Files:**
- Create: `internal/tui/tty.go`
- Test: `internal/tui/tty_test.go`

- [ ] **Step 1: Add the dependency**

Run:
```bash
go get golang.org/x/term@latest
go mod tidy
```

- [ ] **Step 2: Write the failing test (`internal/tui/tty_test.go`, package `tui`)**

```go
package tui

import (
	"os"
	"testing"
)

func TestIsTerminalFalseForPipe(t *testing.T) {
	rd, wr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer rd.Close()
	defer wr.Close()
	if IsTerminal(wr) {
		t.Error("a pipe write end is not a terminal")
	}
}

func TestIsTerminalFalseForRegularFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "f")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if IsTerminal(f) {
		t.Error("a regular file is not a terminal")
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/tui/ -run TestIsTerminal -v`
Expected: FAIL — `undefined: IsTerminal`.

- [ ] **Step 4: Write the implementation (`internal/tui/tty.go`)**

```go
package tui

import (
	"os"

	"golang.org/x/term"
)

// IsTerminal reports whether f is an interactive terminal. The TUI renders only
// when this is true for the stream it draws on (stderr); otherwise the caller
// falls back to the text reporter.
func IsTerminal(f *os.File) bool {
	return f != nil && term.IsTerminal(int(f.Fd()))
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/tui/tty.go internal/tui/tty_test.go
git commit -m "Add TTY detection for reporter selection"
```

---

## Task 6: Wire reporter selection into the CLI

**Files:**
- Modify: `internal/cli/run.go` (`runTasks`, `newRunCmd`, `newPlanCmd` call site)
- Test: `internal/cli/run_test.go`

- [ ] **Step 1: Write the failing test (append to `internal/cli/run_test.go`)**

```go
import (
	"os"
	// ...existing imports...
	"github.com/ravistakumar/fan/internal/run"
	"github.com/ravistakumar/fan/internal/tui"
)

func TestChooseReporterFallsBackToTextWhenPlain(t *testing.T) {
	// plain=true forces the text reporter even if stderr were a terminal.
	r := chooseReporter(os.Stderr, true, func() {})
	if _, ok := r.(*run.TextReporter); !ok {
		t.Errorf("plain should select TextReporter, got %T", r)
	}
}

func TestChooseReporterUsesTextForNonTTY(t *testing.T) {
	// A pipe is not a TTY → text reporter regardless of plain.
	_, wr, _ := os.Pipe()
	defer wr.Close()
	r := chooseReporter(wr, false, func() {})
	if _, ok := r.(*run.TextReporter); !ok {
		t.Errorf("non-TTY should select TextReporter, got %T", r)
	}
}

func TestChooseReporterUsesTUIForTTY(t *testing.T) {
	// Allocating a real PTY isn't available in CI, so skip unless stderr
	// happens to be an interactive terminal; when it is, the TTY path must
	// select the TUI reporter.
	if !tui.IsTerminal(os.Stderr) {
		t.Skip("no TTY available in this environment")
	}
	r := chooseReporter(os.Stderr, false, func() {})
	if _, ok := r.(*tui.Reporter); !ok {
		t.Errorf("TTY should select tui.Reporter, got %T", r)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/cli/ -run TestChooseReporter -v`
Expected: FAIL — `undefined: chooseReporter`.

- [ ] **Step 3: Add `chooseReporter` and rewire `runTasks` in `internal/cli/run.go`**

Add the import for `tui` and a helper, then thread `plain` and a cancellable context through `runTasks`.

Add to the imports block:
```go
	"github.com/ravistakumar/fan/internal/tui"
```

Add the helper:
```go
// chooseReporter returns the live TUI reporter when out is an interactive
// terminal and plain is false; otherwise the text reporter.
func chooseReporter(out *os.File, plain bool, cancel context.CancelFunc) run.Reporter {
	if !plain && tui.IsTerminal(out) {
		return tui.New(out, cancel)
	}
	return run.NewTextReporter(out)
}
```

Replace the body of `runTasks` so it owns the cancellable context, takes `plain`, and selects the reporter:
```go
func runTasks(cmd *cobra.Command, tasks []task.Task, conc int, finalGate string, plain bool) error {
	repo, err := vcs.Open(".")
	if err != nil {
		return err
	}
	workRoot := filepath.Join(repo.Root(), ".fan")
	if err := os.MkdirAll(workRoot, 0o755); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r := run.Runner{
		Repo:      repo,
		WorkRoot:  workRoot,
		Reporter:  chooseReporter(os.Stderr, plain, cancel),
		FinalGate: finalGate,
		Now:       timestamp,
	}
	outcomes, branch, fg, err := r.RunWithFinalGate(ctx, tasks, conc)
	if err != nil {
		return err
	}
	fmt.Fprint(cmd.OutOrStdout(), summary.Render(outcomes, branch, fg))
	return runStatusErr(outcomes, fg)
}
```

- [ ] **Step 4: Add the `--plain` flag and update both call sites**

In `newRunCmd`, add the flag variable and registration, and pass it through:
```go
	var (
		each        string
		agentName   string
		gateCmd     string
		concurrency int
		only        string
		noFinalGate bool
		plain       bool
	)
```
At the end of `RunE`, change the call to:
```go
			return runTasks(cmd, tasks, conc, finalGate, plain)
```
Register the flag alongside the others:
```go
	cmd.Flags().BoolVar(&plain, "plain", false, "force the plain text progress reporter (no TUI)")
```

In `newPlanCmd` (in `internal/cli/plan.go`), the `--yes` branch calls `runTasks`. Update that call to pass `plain=false`:
```go
			return runTasks(cmd, tasks, concOrDefault(conc), firstGate(tasks), false)
```

- [ ] **Step 5: Run the CLI tests and build**

Run: `go test ./internal/cli/ -v && go build -o /tmp/fan ./cmd/fan`
Expected: PASS (including the non-TTY/plain selection tests; the TTY test skips without a PTY) and the binary builds. The existing e2e test (`TestRunCommandEndToEnd`) runs through `chooseReporter`, which returns the text reporter because the test's output is not a TTY — so it still asserts "2 merged".

- [ ] **Step 6: Commit**

```bash
git add internal/cli/run.go internal/cli/plan.go internal/cli/run_test.go
git commit -m "Select live TUI vs text reporter by TTY, add --plain"
```

---

## Task 7: Final verification

**Files:** none (verification only)

- [ ] **Step 1: Full suite with the race detector**

Run: `go test -race ./...`
Expected: PASS, no race warnings. (The reporter Send calls happen from worker goroutines; the program's internal handling is goroutine-safe, and the model is only mutated inside the program's own goroutine.)

- [ ] **Step 2: Vet and lint**

Run: `go vet ./... && golangci-lint run`
Expected: clean, `0 issues`. If `golangci-lint` flags the discarded `program.Run()` error, the `_, _ =` form already ignores it intentionally; match the existing repo idiom (assign to `_`).

- [ ] **Step 3: Manual TUI smoke test (real terminal)**

In a scratch repo with a fake agent on PATH (reuse `docs/demo/render.sh`'s setup or a manual one), run a multi-task fan in a real terminal and confirm:
- the full-screen live view shows running rows with spinner + elapsed and a live counter;
- on completion the screen restores and the normal summary prints;
- `Ctrl-C` mid-run cancels: the screen restores, a partial summary prints, and `git worktree list` shows no leftover `.fan` worktrees;
- `fan run --plain …` shows the old text output;
- `fan run … | cat` (non-TTY) falls back to text.

- [ ] **Step 4: Commit any doc note (optional)**

If desired, note `--plain` in the README's flags. Otherwise no commit.

---

## Done

`fan` now has a live full-screen TUI progress view that activates on an
interactive terminal, falls back cleanly to the text reporter for CI/pipes and
`--plain`, supports graceful Ctrl-C cancellation with no leaked worktrees, and
keeps `summary.Render` as the single source of truth for the final output. The
core `run` package gained only the `Begin`/`End` seam calls and a one-line
cancel guard; all bubbletea code is confined to `internal/tui`.
