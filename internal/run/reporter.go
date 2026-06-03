package run

import (
	"fmt"
	"io"
	"sync"

	"github.com/ravistakumar/fan/internal/result"
	"github.com/ravistakumar/fan/internal/task"
)

// Reporter receives progress events as tasks start and finish. Implementations
// must be safe for concurrent use: Start/Finish are called from worker
// goroutines.
type Reporter interface {
	Start(t task.Task)
	Finish(o result.Outcome)
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
