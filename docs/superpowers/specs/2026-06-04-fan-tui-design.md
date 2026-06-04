# fan — live TUI progress view

**Date:** 2026-06-04
**Status:** Design approved, ready for implementation planning

## Summary

`fan` currently reports progress with a plain-text, append-only reporter (`▶`
on start, `•` on finish). This adds an opt-in, full-screen **live TUI** that
shows running tasks updating in place — spinner, agent, and elapsed time per
task — with rolled-up status counts, then restores the screen and prints the
existing summary. It is a second implementation of the existing `run.Reporter`
seam; the core `run` package stays unchanged except for two lifecycle calls.

The TUI activates automatically when stderr is an interactive terminal and falls
back to the text reporter otherwise (CI, pipes, `--plain`).

## Goals

- A live, full-screen progress view during a run: per-task rows for the
  currently-running tasks, a short list of recent completions, and a live
  counter.
- Graceful cancellation (`ctrl+c` / `q`): stop launching tasks, kill in-flight
  agents, tear down worktrees, restore the screen, and print a partial summary.
- Keep the core `run` package free of TUI dependencies; keep `summary.Render`
  as the single source of truth for the final output.

## Non-goals

- Replacing the text reporter (it remains the default for non-TTY / `--plain`,
  and the demo).
- A new terminal status type for cancellation (canceled tasks reuse `Errored`).
- Re-rendering the demo GIF (a follow-up, noted below).
- Scrollback persistence of the live view (full-screen view is transient by
  design; the summary that prints after is the persistent record).

## Layout

Full-screen (alternate screen) during the run. The running set is bounded by the
concurrency cap, so a row per running task always fits; remaining height holds a
capped "recently done" list, so the view never overflows for any task count.

```
  fan · 42 tasks · up to 8 in parallel

  running
    ⠹ src/Modal.tsx   claude   3.2s
    ⠴ src/Tabs.tsx    codex    1.1s
  recently done
    ✓ src/Button.tsx  merged
    ✗ src/Input.tsx   gate-fail

  18/42 done · 15 merged · 2 gate-fail · 1 conflict
```

`lipgloss` colors status: green `merged`, red `error`/`gate-fail`, yellow
`conflict`, dim `no-op`. On cancel the header shows `canceling…` until teardown.

## Architecture

### Package layout

```
internal/run/reporter.go   Reporter interface (+ Begin/End); TextReporter (no-op Begin/End)
internal/run/run.go        execute() calls Reporter.Begin/End; runTask() ctx guard
internal/tui/reporter.go    tui.Reporter — implements run.Reporter, owns the tea.Program
internal/tui/model.go       bubbletea model: state + Update + View (pure, unit-tested)
internal/tui/tty.go         IsTerminal(*os.File) bool
internal/cli/run.go        reporter selection (TTY / --plain) + ctx/cancel wiring
```

### Dependency direction

```
cli ─→ run         (core: scheduler, vcs, gate — NO bubbletea)
cli ─→ tui ─→ {task, result}    (bubbletea lives ONLY in internal/tui)
```

`internal/tui` does **not** import `run`; it satisfies `run.Reporter`
structurally. This keeps bubbletea out of the core and out of the run-package
tests. A compile-time `var _ run.Reporter = (*tui.Reporter)(nil)` assertion
guards conformance.

### New dependencies

`github.com/charmbracelet/bubbletea`, `github.com/charmbracelet/lipgloss`,
`golang.org/x/term` (TTY detection). Test-only:
`github.com/charmbracelet/x/exp/teatest`. All bubbletea/lipgloss imports are
confined to `internal/tui`.

## The Reporter interface

```go
type Reporter interface {
	Begin(total, concurrency int) // once, before the run
	Start(t task.Task)            // task picked up by a worker
	Finish(o result.Outcome)      // task reached a terminal status
	End()                         // once, after the run (restores screen)
}
```

- `TextReporter.Begin`/`End` are no-ops — today's `▶`/`•` output is unchanged.
- `run.go`'s `execute()` calls `r.Reporter.Begin(len(tasks), concurrency)` at the
  top and `defer r.Reporter.End()`.
- `Start`/`Finish` keep their current call sites and concurrency-safety contract.

## Threading and lifecycle (internal/tui)

The runner is synchronous, so the program runs in a goroutine:

- **`Begin`**: build the model; `program = tea.NewProgram(model, tea.WithAltScreen(), tea.WithOutput(stderr))`; `go func(){ _, _ = program.Run(); close(done) }()`.
- **`Start` / `Finish`**: `program.Send(startMsg{id, agent})` / `program.Send(finishMsg{outcome})`. `Send` is goroutine-safe, so worker goroutines call these directly.
- **`End`**: `program.Quit()`, then `<-done`. Blocking on `done` guarantees the
  program has exited and the terminal is restored **before** the CLI writes the
  summary to stdout. `End` is idempotent (a second call, e.g. after a
  user-initiated cancel already quit the program, returns immediately).

## Cancellation

- The CLI creates `ctx, cancel := context.WithCancel(...)` and passes `cancel`
  into `tui.New(stderr, cancel)`. The model holds it.
- On `ctrl+c` / `q`, `Update` sets `canceling = true`, calls `cancel()`, and
  returns `tea.Quit`.
- Cancellation propagates into the runner:
  - in-flight agents die via the existing `exec.CommandContext(ctx)` in
    `agent.Run`;
  - a one-line guard at the top of `runTask` — `if ctx.Err() != nil { return a
    canceled candidate }` — short-circuits not-yet-started tasks.
- Canceled tasks surface as `Errored` with detail `"canceled"` (no new status;
  `result`, `Decide`, and `summary` are untouched).
- Because cancel lets the runner return normally (rather than `os.Exit`), the
  existing `defer` cleanup removes all worktrees — no leaked `.fan` worktrees.

## The bubbletea model (internal/tui/model.go)

### State

```go
type taskState struct {
	id, agent string
	status    result.Status
	started   time.Time
	done      bool
}

type model struct {
	total, concurrency int
	cancel             context.CancelFunc
	now                time.Time // updated by each tick → View stays pure/testable
	frame              int       // spinner frame index
	order              []string  // running ids, in start order
	running            map[string]*taskState
	recent             []taskState // completed, newest last; capped to fit height
	counts             map[result.Status]int
	done               int
	width, height      int
	canceling          bool
}
```

### Messages and Update (pure)

| message | effect |
| --- | --- |
| `tea.WindowSizeMsg` | store `width`, `height` |
| `tickMsg{t}` (~100ms) | `now = t`; `frame++`; re-issue tick command |
| `startMsg{id, agent}` | add to `running` / `order` with `started = now` |
| `finishMsg{outcome}` | remove from `running`; `counts[status]++`; `done++`; append to `recent` |
| `tea.KeyMsg` ctrl+c / `q` | `canceling = true`; call `cancel()`; return `tea.Quit` |

- `Init()` returns the first tick command.
- Spinner is a hand-rolled frame set `⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏` (no `bubbles` dependency).
- Elapsed per task = `now.Sub(started)`; injecting fixed-time `tickMsg`s makes
  `View` output deterministic for tests.

### View

Renders the layout above. Height budget: header (2) + `running` label (1) +
running rows (≤ concurrency) + `recently done` label (1) + counter (2); the
`recently done` list is truncated to the rows that remain, so the view fits any
terminal height and any task count.

## CLI selection (internal/cli/run.go)

`runTasks` gains a `plain bool` parameter (and now owns the cancellable
context). `fan run` passes its `--plain` flag value; `fan plan --yes` passes
`false`. The body:

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

var reporter run.Reporter
if !plain && tui.IsTerminal(os.Stderr) {
	reporter = tui.New(os.Stderr, cancel)
} else {
	reporter = run.NewTextReporter(cmd.ErrOrStderr())
}
r := run.Runner{ /* … */ Reporter: reporter }
outcomes, branch, fg, err := r.RunWithFinalGate(ctx, tasks, conc)
if err != nil {
	return err
}
fmt.Fprint(cmd.OutOrStdout(), summary.Render(outcomes, branch, fg))
return runStatusErr(outcomes, fg)
```

- **TTY detection** checks **stderr** (where the TUI renders), so redirecting
  stdout does not disable it; a non-interactive environment falls back to text.
- **`--plain`** flag on `fan run` (and inherited by `fan plan --yes` through the
  shared `runTasks`) forces `TextReporter`.
- TUI renders to stderr; `summary.Render` and any piped output go to stdout.

## Testing

- **`internal/tui/model_test.go`** (the bulk): drive `model.Update` with message
  sequences and assert `View()`. Deterministic via injected `tickMsg{t}`. Covers:
  start → running row with spinner/agent/elapsed; finish → moves to recent,
  updates counts and `done/total`, correct glyph/color; counter matches tallies;
  `ctrl+c` sets `canceling`, calls the injected cancel func, returns `tea.Quit`;
  small height truncates `recently done` while running rows remain.
- **`internal/tui/reporter_test.go`**: one `teatest` integration test driving a
  full `Begin → Start → Finish → End` cycle, asserting rendered frames and clean
  teardown; plus the compile-time `run.Reporter` conformance assertion.
- **`internal/tui/tty_test.go`**: `IsTerminal` returns false for an `os.Pipe` /
  regular file (the CI fallback path).
- **`internal/run/run_test.go`**: add a `Reporter` spy; assert `execute` calls
  `Begin` once, `End` once, and `Start`/`Finish` once per task. Existing 7 tests
  stay green (`TextReporter` no-op `Begin`/`End`).
- **Manual pre-merge:** real-terminal run shows the live view; `ctrl+c` cancels
  and `git worktree list` is clean afterward; `--plain` forces text; `fan run …
  | cat` falls back to text.

## Follow-ups (out of scope)

- Re-render `docs/demo.gif` to showcase the TUI (vhs runs in a real PTY), or keep
  `--plain` for the current text demo.
