package run

import (
	"fmt"
	"io"
	"sync"

	"github.com/ravistakumar/fan/internal/result"
	"github.com/ravistakumar/fan/internal/task"
)

// Reporter receives progress events over the lifetime of a run. Implementations
// must be safe for concurrent use: Start/Finish are called from worker
// goroutines. Begin and End bracket the run and are called once each.
type Reporter interface {
	Begin(total, concurrency int)
	Start(t task.Task)
	Finish(o result.Outcome)
	End()
}

// TextReporter writes one line per event to a writer, serialized by a mutex.
type TextReporter struct {
	mu sync.Mutex
	w  io.Writer
}

// NewTextReporter returns a TextReporter writing to w.
func NewTextReporter(w io.Writer) *TextReporter {
	return &TextReporter{w: w}
}

func (r *TextReporter) Start(t task.Task) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fmt.Fprintf(r.w, "▶ %-16s %s\n", t.ID, t.Agent)
}

func (r *TextReporter) Finish(o result.Outcome) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fmt.Fprintf(r.w, "• %-16s %s\n", o.Task.ID, o.Status)
}

// Begin and End are no-ops for the text reporter; its output is purely the
// per-event lines from Start/Finish.
func (r *TextReporter) Begin(total, concurrency int) {}
func (r *TextReporter) End()                         {}
