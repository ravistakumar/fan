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
