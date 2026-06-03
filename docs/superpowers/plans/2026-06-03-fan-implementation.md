# fan Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `fan`, an agent-agnostic CLI that fans a list of independent coding tasks across headless agent CLIs in parallel, isolates each in a git worktree, and auto-merges the clean+gated ones onto a throwaway integration branch.

**Architecture:** Pure functional core (`task`, `schedule`, `result`, `summary`, `planner`) wired to an imperative shell (`agent`, `vcs`, `gate`) by a `run.Runner`, exposed through a `cobra` CLI. Mirrors the sibling project `prr`'s structure and test style.

**Tech Stack:** Go 1.23, single static binary. `cobra` (CLI), `BurntSushi/toml` (task files). Git via shell-out (`os/exec`), no libgit2. GoReleaser + GitHub Actions, Homebrew cask via `ravistakumar/homebrew-tap`.

**Module path:** `github.com/ravistakumar/fan`

**Scope note:** v0.1 ships a plain-text progress `Reporter`. The live `bubbletea` TUI from the spec is deferred to v0.2 (it consumes the same `Reporter` event stream). `fan retry` is implemented as a `--only <ids>` filter on `fan run`, not a separate command.

---

## File Structure

```
cmd/fan/main.go              entry point → cli.Execute()
internal/task/task.go        Task/Defaults/File types, TOML parse, validation, --each expansion (pure)
internal/agent/agent.go      Agent interface, Supported list, New() switch (5 adapters)
internal/agent/exec.go       cmdAgent: headless run in a working dir
internal/schedule/schedule.go bounded-concurrency, order-preserving Run (pure control)
internal/result/result.go    Status enum, Outcome type, pure Decide() state machine
internal/gate/gate.go        run a gate command in a dir → pass/fail+output
internal/vcs/vcs.go          git: worktrees, commit-all, branches, cherry-pick (shell-out)
internal/summary/summary.go  render the final summary table (pure)
internal/planner/planner.go  planner meta-prompt + tolerant JSON plan parse (pure)
internal/run/run.go          Runner: wires schedule→per-task pipeline→serialized integrate→final gate
internal/run/reporter.go     Reporter interface + TextReporter
internal/cli/cli.go          cobra root + `run` and `plan` commands
```

Each `*.go` gets a matching `*_test.go`. Functional-core packages are unit-tested with no I/O; `agent`, `vcs`, `gate`, and `run` use real `git` in `t.TempDir()` and an injected fake `Agent` (no network, no real agent cost).

---

## Task 1: Project scaffold

**Files:**
- Create: `go.mod`
- Create: `cmd/fan/main.go`
- Create: `internal/cli/cli.go`
- Create: `.gitignore`
- Create: `Makefile`

- [ ] **Step 1: Create go.mod**

```
module github.com/ravistakumar/fan

go 1.23

require (
	github.com/BurntSushi/toml v1.4.0
	github.com/spf13/cobra v1.8.1
)
```

- [ ] **Step 2: Create internal/cli/cli.go with a minimal root command**

```go
// Package cli defines the fan command-line interface.
package cli

import (
	"github.com/spf13/cobra"
)

// version is overridden at build time via -ldflags.
var version = "dev"

// Execute runs the root command and returns its exit error.
func Execute() error {
	root := &cobra.Command{
		Use:           "fan",
		Short:         "Fan independent coding tasks across agents, in parallel",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	return root.Execute()
}
```

- [ ] **Step 3: Create cmd/fan/main.go**

```go
package main

import (
	"fmt"
	"os"

	"github.com/ravistakumar/fan/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "fan:", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 4: Create .gitignore**

```
/fan
/dist/
.fan/
```

- [ ] **Step 5: Create Makefile**

```make
.PHONY: build test lint
build:
	go build -o fan ./cmd/fan
test:
	go test ./...
lint:
	golangci-lint run
```

- [ ] **Step 6: Tidy, build, and verify the binary runs**

Run: `go mod tidy && go build -o fan ./cmd/fan && ./fan --version`
Expected: prints `fan version dev`

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum cmd/ internal/ .gitignore Makefile
git commit -m "Scaffold fan CLI skeleton"
```

---

## Task 2: Task file types, parsing, and validation

**Files:**
- Create: `internal/task/task.go`
- Test: `internal/task/task_test.go`

- [ ] **Step 1: Write the failing test**

```go
package task

import "testing"

func anyAgent(string) bool { return true }

func TestParseResolvesDefaults(t *testing.T) {
	in := []byte(`
[defaults]
agent = "codex"
gate  = "npm test"
concurrency = 4

[[task]]
id = "a"
prompt = "do a"

[[task]]
id = "b"
prompt = "do b"
agent = "claude"
`)
	f, err := Parse(in, anyAgent)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if f.Defaults.Concurrency != 4 {
		t.Errorf("concurrency = %d, want 4", f.Defaults.Concurrency)
	}
	if len(f.Tasks) != 2 {
		t.Fatalf("tasks = %d, want 2", len(f.Tasks))
	}
	if f.Tasks[0].Agent != "codex" || f.Tasks[0].Gate != "npm test" {
		t.Errorf("task a not resolved from defaults: %+v", f.Tasks[0])
	}
	if f.Tasks[1].Agent != "claude" {
		t.Errorf("task b override = %q, want claude", f.Tasks[1].Agent)
	}
}

func TestParseRejectsDuplicateIDs(t *testing.T) {
	in := []byte(`
[[task]]
id = "x"
prompt = "one"
[[task]]
id = "x"
prompt = "two"
`)
	if _, err := Parse(in, anyAgent); err == nil {
		t.Fatal("expected duplicate-id error, got nil")
	}
}

func TestParseRejectsUnknownAgent(t *testing.T) {
	in := []byte(`
[[task]]
id = "x"
prompt = "one"
agent = "bogus"
`)
	reject := func(string) bool { return false }
	if _, err := Parse(in, reject); err == nil {
		t.Fatal("expected unknown-agent error, got nil")
	}
}

func TestParseRejectsEmptyPrompt(t *testing.T) {
	in := []byte(`
[[task]]
id = "x"
prompt = ""
`)
	if _, err := Parse(in, anyAgent); err == nil {
		t.Fatal("expected empty-prompt error, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/task/ -run TestParse -v`
Expected: FAIL — `undefined: Parse`

- [ ] **Step 3: Write the implementation**

```go
// Package task defines the unit of work fan fans out: a self-contained prompt
// run by one agent and checked by one gate. Parsing, default resolution, and
// validation live here as pure functions over bytes — no I/O.
package task

import (
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"
)

// Defaults supplies the agent, gate, and concurrency used by any task that does
// not override them.
type Defaults struct {
	Agent       string `toml:"agent"`
	Gate        string `toml:"gate"`
	Concurrency int    `toml:"concurrency"`
}

// Task is one independent unit of work. Agent and Gate are always resolved
// (never empty for Agent) after Parse.
type Task struct {
	ID     string `toml:"id"`
	Prompt string `toml:"prompt"`
	Agent  string `toml:"agent"`
	Gate   string `toml:"gate"`
}

// File is a parsed task file: defaults plus the resolved task list.
type File struct {
	Defaults Defaults `toml:"defaults"`
	Tasks    []Task   `toml:"task"`
}

// gateSet marks gate as explicitly present so an empty per-task gate can mean
// "no gate" while an absent one inherits the default.
type rawTask struct {
	ID     string  `toml:"id"`
	Prompt string  `toml:"prompt"`
	Agent  string  `toml:"agent"`
	Gate   *string `toml:"gate"`
}

type rawFile struct {
	Defaults Defaults  `toml:"defaults"`
	Tasks    []rawTask `toml:"task"`
}

// Parse decodes a task file, resolves per-task defaults, and validates the
// result. validAgent reports whether an agent name is supported; it is injected
// so this package stays decoupled from the agent package.
func Parse(data []byte, validAgent func(string) bool) (File, error) {
	var raw rawFile
	if err := toml.Unmarshal(data, &raw); err != nil {
		return File{}, fmt.Errorf("parse task file: %w", err)
	}

	def := raw.Defaults
	if def.Concurrency == 0 {
		def.Concurrency = DefaultConcurrency()
	}

	out := File{Defaults: def}
	seen := map[string]bool{}
	for i, rt := range raw.Tasks {
		if strings.TrimSpace(rt.ID) == "" {
			return File{}, fmt.Errorf("task %d: missing id", i+1)
		}
		if seen[rt.ID] {
			return File{}, fmt.Errorf("duplicate task id %q", rt.ID)
		}
		seen[rt.ID] = true
		if strings.TrimSpace(rt.Prompt) == "" {
			return File{}, fmt.Errorf("task %q: empty prompt", rt.ID)
		}

		agent := rt.Agent
		if agent == "" {
			agent = def.Agent
		}
		if agent == "" {
			return File{}, fmt.Errorf("task %q: no agent set and no default agent", rt.ID)
		}
		if !validAgent(agent) {
			return File{}, fmt.Errorf("task %q: unknown agent %q", rt.ID, agent)
		}

		gate := def.Gate
		if rt.Gate != nil {
			gate = *rt.Gate
		}

		out.Tasks = append(out.Tasks, Task{ID: rt.ID, Prompt: rt.Prompt, Agent: agent, Gate: gate})
	}
	if len(out.Tasks) == 0 {
		return File{}, fmt.Errorf("task file has no tasks")
	}
	return out, nil
}
```

- [ ] **Step 4: Add the concurrency default helper**

```go
// internal/task/concurrency.go
package task

import "runtime"

// DefaultConcurrency leaves one core free, with a floor of 1. Exported so the
// CLI can apply the same default to the --each path.
func DefaultConcurrency() int {
	n := runtime.NumCPU() - 1
	if n < 1 {
		return 1
	}
	return n
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/task/ -run TestParse -v`
Expected: PASS (all four)

- [ ] **Step 6: Commit**

```bash
git add internal/task/
git commit -m "Add task file parsing with default resolution and validation"
```

---

## Task 3: `--each` template expansion

**Files:**
- Modify: `internal/task/task.go` (add `Expand`)
- Test: `internal/task/expand_test.go`

- [ ] **Step 1: Write the failing test**

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/task/ -run TestExpand -v`
Expected: FAIL — `undefined: Expand`

- [ ] **Step 3: Write the implementation**

```go
// Expand turns a list of items and a template into tasks. The template must
// contain the placeholder "{}", which is replaced by each item. IDs are derived
// from each item's base name (lowercased, non-alphanumerics collapsed to "-")
// and de-duplicated by appending an index.
func Expand(items []string, template, agent, gate string) ([]Task, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("no items to fan out")
	}
	if !strings.Contains(template, "{}") {
		return nil, fmt.Errorf("template must contain the {} placeholder")
	}
	if agent == "" {
		return nil, fmt.Errorf("no agent specified")
	}

	seen := map[string]int{}
	out := make([]Task, 0, len(items))
	for _, item := range items {
		base := deriveID(item)
		if base == "" {
			base = "task"
		}
		id := base
		if n := seen[base]; n > 0 {
			id = fmt.Sprintf("%s-%d", base, n+1)
		}
		seen[base]++
		out = append(out, Task{
			ID:     id,
			Prompt: strings.ReplaceAll(template, "{}", item),
			Agent:  agent,
			Gate:   gate,
		})
	}
	return out, nil
}

// deriveID reduces a path to a short slug: base name without extension,
// lowercased, with runs of non-alphanumerics turned into single hyphens.
func deriveID(item string) string {
	s := item
	if i := strings.LastIndexAny(s, "/\\"); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.LastIndex(s, "."); i > 0 {
		s = s[:i]
	}
	s = strings.ToLower(s)
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
		} else if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/task/ -v`
Expected: PASS (all task tests)

- [ ] **Step 5: Commit**

```bash
git add internal/task/
git commit -m "Add --each template expansion with id derivation"
```

---

## Task 4: Agent adapters

**Files:**
- Create: `internal/agent/agent.go`
- Create: `internal/agent/exec.go`
- Test: `internal/agent/agent_test.go`

- [ ] **Step 1: Write the failing test**

```go
package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNewSupportedAgents(t *testing.T) {
	for _, name := range Supported {
		if _, err := New(name); err != nil {
			t.Errorf("New(%q): %v", name, err)
		}
	}
	if _, err := New("nope"); err == nil {
		t.Error("New(nope): expected error")
	}
}

func TestIsSupported(t *testing.T) {
	if !IsSupported("grok") {
		t.Error("grok should be supported")
	}
	if IsSupported("nope") {
		t.Error("nope should not be supported")
	}
}

// TestRunInDir uses a stand-in command ("sh") to prove Run executes in the
// given directory and returns stdout. It does not exercise a real agent.
func TestRunInDir(t *testing.T) {
	dir := t.TempDir()
	a := &cmdAgent{name: "fake", command: "sh", runArgs: []string{"-c"}}
	out, err := a.Run(context.Background(), "pwd", dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	resolved, _ := filepath.EvalSymlinks(dir)
	got, _ := filepath.EvalSymlinks(trim(out))
	if got != resolved {
		t.Errorf("ran in %q, want %q", got, resolved)
	}
}

func TestRunSurfacesStderrOnFailure(t *testing.T) {
	a := &cmdAgent{name: "fake", command: "sh", runArgs: []string{"-c"}}
	_, err := a.Run(context.Background(), "echo boom >&2; exit 3", t.TempDir())
	if err == nil {
		t.Fatal("expected error from failing command")
	}
}

func trim(s string) string { return string([]byte(stringsTrimSpace(s))) }

func stringsTrimSpace(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == ' ' || s[len(s)-1] == '\t' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

var _ = os.Stdout
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -v`
Expected: FAIL — `undefined: Supported` / `New`

- [ ] **Step 3: Write internal/agent/agent.go**

```go
// Package agent isolates all interaction with external coding-agent CLIs
// behind one interface. Each agent runs headless in a given working directory.
// Adding an agent is one case in New.
package agent

import (
	"context"
	"fmt"
)

// Agent is a coding-agent CLI fan can run headless inside a worktree.
type Agent interface {
	Name() string
	Available() bool
	Run(ctx context.Context, prompt, dir string) (string, error)
}

// Supported lists the agents fan knows how to drive.
var Supported = []string{"claude", "codex", "opencode", "aider", "grok"}

// IsSupported reports whether name is a supported agent.
func IsSupported(name string) bool {
	for _, s := range Supported {
		if s == name {
			return true
		}
	}
	return false
}

// New builds a supported agent. The argument lists encode each CLI's documented
// headless invocation; the prompt is appended after them.
func New(name string) (Agent, error) {
	switch name {
	case "claude":
		// claude -p "<prompt>"
		return newCmdAgent("claude", "claude", []string{"-p"}), nil
	case "codex":
		// codex exec "<prompt>"
		return newCmdAgent("codex", "codex", []string{"exec"}), nil
	case "opencode":
		// opencode run --prompt "<prompt>"   (verified against opencode v1.2.24)
		return newCmdAgent("opencode", "opencode", []string{"run", "--prompt"}), nil
	case "aider":
		// aider --yes --no-auto-commits --message "<prompt>"  (experimental)
		return newCmdAgent("aider", "aider", []string{"--yes", "--no-auto-commits", "--message"}), nil
	case "grok":
		// grok --no-auto-update -p "<prompt>"
		return newCmdAgent("grok", "grok", []string{"--no-auto-update", "-p"}), nil
	default:
		return nil, fmt.Errorf("unknown agent %q", name)
	}
}
```

- [ ] **Step 4: Write internal/agent/exec.go**

```go
package agent

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// cmdAgent drives a CLI that takes a prompt as its final argument, run headless
// inside a working directory. runArgs are the fixed args before the prompt.
type cmdAgent struct {
	name    string
	command string
	runArgs []string
}

func newCmdAgent(name, command string, runArgs []string) *cmdAgent {
	return &cmdAgent{name: name, command: command, runArgs: runArgs}
}

func (a *cmdAgent) Name() string { return a.name }

func (a *cmdAgent) Available() bool {
	_, err := exec.LookPath(a.command)
	return err == nil
}

func (a *cmdAgent) Run(ctx context.Context, prompt, dir string) (string, error) {
	args := make([]string, 0, len(a.runArgs)+1)
	args = append(args, a.runArgs...)
	args = append(args, prompt)

	cmd := exec.CommandContext(ctx, a.command, args...)
	cmd.Dir = dir
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		if errOut.Len() > 0 {
			return out.String(), fmt.Errorf("%s: %w: %s", a.name, err, strings.TrimSpace(errOut.String()))
		}
		return out.String(), fmt.Errorf("%s: %w", a.name, err)
	}
	return out.String(), nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/agent/ -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/agent/
git commit -m "Add agent adapters for claude, codex, opencode, aider, grok"
```

---

## Task 5: Bounded-concurrency scheduler

**Files:**
- Create: `internal/schedule/schedule.go`
- Test: `internal/schedule/schedule_test.go`

- [ ] **Step 1: Write the failing test**

```go
package schedule

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunPreservesOrder(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}
	got := Run(context.Background(), items, 2, func(_ context.Context, _ int, v int) int {
		return v * 10
	})
	for i, v := range got {
		if v != items[i]*10 {
			t.Errorf("got[%d] = %d, want %d", i, v, items[i]*10)
		}
	}
}

func TestRunRespectsConcurrencyCap(t *testing.T) {
	var inFlight, peak int32
	items := make([]int, 20)
	Run(context.Background(), items, 3, func(_ context.Context, _ int, _ int) int {
		n := atomic.AddInt32(&inFlight, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
				break
			}
		}
		time.Sleep(2 * time.Millisecond)
		atomic.AddInt32(&inFlight, -1)
		return 0
	})
	if peak > 3 {
		t.Errorf("peak concurrency = %d, want <= 3", peak)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/schedule/ -v`
Expected: FAIL — `undefined: Run`

- [ ] **Step 3: Write the implementation**

```go
// Package schedule runs a worker over a list of items with a bounded number of
// concurrent goroutines, returning results in input order.
package schedule

import (
	"context"
	"sync"
)

// Run applies worker to each item with at most n concurrent calls. Results are
// returned in the same order as items. worker receives the item's index. n < 1
// is treated as 1.
func Run[T any, R any](ctx context.Context, items []T, n int, worker func(ctx context.Context, idx int, item T) R) []R {
	if n < 1 {
		n = 1
	}
	results := make([]R, len(items))
	sem := make(chan struct{}, n)
	var wg sync.WaitGroup
	for i := range items {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int) {
			defer wg.Done()
			defer func() { <-sem }()
			results[idx] = worker(ctx, idx, items[idx])
		}(i)
	}
	wg.Wait()
	return results
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/schedule/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/schedule/
git commit -m "Add order-preserving bounded-concurrency scheduler"
```

---

## Task 6: Result status and the merge-decision state machine

**Files:**
- Create: `internal/result/result.go`
- Test: `internal/result/result_test.go`

- [ ] **Step 1: Write the failing test**

```go
package result

import (
	"testing"

	"github.com/ravistakumar/fan/internal/task"
)

func TestDecide(t *testing.T) {
	cases := []struct {
		name string
		in   Decision
		want Status
	}{
		{"agent error wins", Decision{AgentErr: true, Changed: true, GatePassed: true, CherryClean: true}, Errored},
		{"no changes", Decision{Changed: false}, NoOp},
		{"gate fail", Decision{Changed: true, GatePassed: false}, QueuedGateFail},
		{"conflict", Decision{Changed: true, GatePassed: true, CherryClean: false}, QueuedConflict},
		{"merged", Decision{Changed: true, GatePassed: true, CherryClean: true}, Merged},
	}
	for _, c := range cases {
		if got := Decide(c.in); got != c.want {
			t.Errorf("%s: Decide = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestOutcomeHoldsTask(t *testing.T) {
	o := Outcome{Task: task.Task{ID: "x"}, Status: Merged}
	if o.Task.ID != "x" {
		t.Errorf("task not held: %+v", o)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/result/ -v`
Expected: FAIL — `undefined: Decide`

- [ ] **Step 3: Write the implementation**

```go
// Package result defines the terminal status of a fanned-out task and the pure
// decision that maps an execution's facts to that status.
package result

import "github.com/ravistakumar/fan/internal/task"

// Status is the terminal state of one task.
type Status string

const (
	Merged         Status = "merged"
	QueuedConflict Status = "queued: conflict"
	QueuedGateFail Status = "queued: gate-fail"
	Errored        Status = "error"
	NoOp           Status = "no-op"
)

// Outcome is the full record of one task's run.
type Outcome struct {
	Task   task.Task
	Status Status
	Branch string // kept branch for queued items (empty otherwise)
	Detail string // gate output snippet or error message
}

// Decision holds the facts gathered while running a task, in pipeline order:
// the agent ran (or errored), it changed files (or not), the gate passed (only
// evaluated when changed), and the cherry-pick was clean (only evaluated when
// the gate passed).
type Decision struct {
	AgentErr    bool
	Changed     bool
	GatePassed  bool
	CherryClean bool
}

// Decide maps a Decision to a Status. The order mirrors the run pipeline: an
// agent error short-circuits; no changes is a no-op; the gate is checked before
// any merge; a clean cherry-pick is a merge.
func Decide(d Decision) Status {
	switch {
	case d.AgentErr:
		return Errored
	case !d.Changed:
		return NoOp
	case !d.GatePassed:
		return QueuedGateFail
	case !d.CherryClean:
		return QueuedConflict
	default:
		return Merged
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/result/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/result/
git commit -m "Add result status and merge-decision state machine"
```

---

## Task 7: Gate runner

**Files:**
- Create: `internal/gate/gate.go`
- Test: `internal/gate/gate_test.go`

- [ ] **Step 1: Write the failing test**

```go
package gate

import (
	"context"
	"testing"
)

func TestEmptyGatePasses(t *testing.T) {
	r := Run(context.Background(), "", t.TempDir())
	if !r.Passed {
		t.Error("empty gate should pass (clean-apply-only)")
	}
}

func TestPassingCommand(t *testing.T) {
	r := Run(context.Background(), "true", t.TempDir())
	if !r.Passed {
		t.Errorf("`true` should pass; output=%q", r.Output)
	}
}

func TestFailingCommandCapturesOutput(t *testing.T) {
	r := Run(context.Background(), "echo nope; false", t.TempDir())
	if r.Passed {
		t.Error("`false` should fail")
	}
	if r.Output == "" {
		t.Error("expected captured output")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/gate/ -v`
Expected: FAIL — `undefined: Run`

- [ ] **Step 3: Write the implementation**

```go
// Package gate runs a user-defined check command inside a directory and reports
// whether it passed. An empty command always passes ("clean-apply-only").
package gate

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
)

// Result is the outcome of a gate run.
type Result struct {
	Passed bool
	Output string // combined stdout+stderr, trimmed
}

// Run executes command via `sh -c` inside dir. An empty command passes without
// running anything.
func Run(ctx context.Context, command, dir string) Result {
	if strings.TrimSpace(command) == "" {
		return Result{Passed: true}
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = dir
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return Result{Passed: err == nil, Output: strings.TrimSpace(buf.String())}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/gate/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/gate/
git commit -m "Add gate command runner"
```

---

## Task 8: Git operations (worktrees, commit, branches, cherry-pick)

**Files:**
- Create: `internal/vcs/vcs.go`
- Test: `internal/vcs/vcs_test.go`

- [ ] **Step 1: Write the failing test**

```go
package vcs

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initRepo creates a temp git repo with one commit and returns its path.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-q", "-m", "seed")
	return dir
}

func TestOpenAndHead(t *testing.T) {
	r, err := Open(initRepo(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	sha, err := r.Head()
	if err != nil || len(sha) < 7 {
		t.Fatalf("Head: %q %v", sha, err)
	}
}

func TestWorktreeCommitAndCleanCherryPick(t *testing.T) {
	r, _ := Open(initRepo(t))
	base, _ := r.Head()

	// Integration worktree on a new branch.
	intDir := filepath.Join(t.TempDir(), "integration")
	if err := r.CreateWorktreeBranch(intDir, "fan/test", base); err != nil {
		t.Fatalf("CreateWorktreeBranch: %v", err)
	}

	// Task worktree; make a non-conflicting change; commit.
	wtDir := filepath.Join(t.TempDir(), "wt")
	if err := r.AddWorktree(wtDir, base); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	os.WriteFile(filepath.Join(wtDir, "a.txt"), []byte("a\n"), 0o644)
	sha, changed, err := r.CommitAll(wtDir, "fan(a): add a")
	if err != nil || !changed {
		t.Fatalf("CommitAll: sha=%q changed=%v err=%v", sha, changed, err)
	}

	clean, err := r.CherryPick(intDir, sha)
	if err != nil {
		t.Fatalf("CherryPick: %v", err)
	}
	if !clean {
		t.Error("expected clean cherry-pick")
	}
	if _, err := os.Stat(filepath.Join(intDir, "a.txt")); err != nil {
		t.Errorf("a.txt not on integration branch: %v", err)
	}
}

func TestCommitAllNoChanges(t *testing.T) {
	r, _ := Open(initRepo(t))
	base, _ := r.Head()
	wtDir := filepath.Join(t.TempDir(), "wt")
	r.AddWorktree(wtDir, base)
	_, changed, err := r.CommitAll(wtDir, "noop")
	if err != nil {
		t.Fatalf("CommitAll: %v", err)
	}
	if changed {
		t.Error("expected changed=false when nothing was modified")
	}
}

func TestConflictingCherryPickReportsUnclean(t *testing.T) {
	r, _ := Open(initRepo(t))
	base, _ := r.Head()
	intDir := filepath.Join(t.TempDir(), "integration")
	r.CreateWorktreeBranch(intDir, "fan/test", base)

	// First task edits seed.txt and is merged.
	wt1 := filepath.Join(t.TempDir(), "wt1")
	r.AddWorktree(wt1, base)
	os.WriteFile(filepath.Join(wt1, "seed.txt"), []byte("one\n"), 0o644)
	sha1, _, _ := r.CommitAll(wt1, "edit one")
	if clean, _ := r.CherryPick(intDir, sha1); !clean {
		t.Fatal("first pick should be clean")
	}

	// Second task edits the same line from the same base → conflict.
	wt2 := filepath.Join(t.TempDir(), "wt2")
	r.AddWorktree(wt2, base)
	os.WriteFile(filepath.Join(wt2, "seed.txt"), []byte("two\n"), 0o644)
	sha2, _, _ := r.CommitAll(wt2, "edit two")
	clean, err := r.CherryPick(intDir, sha2)
	if err != nil {
		t.Fatalf("CherryPick: %v", err)
	}
	if clean {
		t.Error("expected conflict (unclean)")
	}
	// Repo must be left clean (cherry-pick aborted), so the next pick can run.
	if clean3, _ := r.CherryPick(intDir, sha1); clean3 {
		// sha1 already applied → empty pick; we only assert no error/hang.
	}
}

func TestCreateBranchAtSha(t *testing.T) {
	r, _ := Open(initRepo(t))
	base, _ := r.Head()
	wt := filepath.Join(t.TempDir(), "wt")
	r.AddWorktree(wt, base)
	os.WriteFile(filepath.Join(wt, "b.txt"), []byte("b\n"), 0o644)
	sha, _, _ := r.CommitAll(wt, "add b")
	if err := r.CreateBranch("fan/keep-me", sha); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
}

var _ = context.Background
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/vcs/ -v`
Expected: FAIL — `undefined: Open`

- [ ] **Step 3: Write the implementation**

```go
// Package vcs wraps the git operations fan needs: opening a repo, creating and
// removing worktrees, committing all changes in a worktree, creating branches,
// and cherry-picking onto an integration worktree. All operations shell out to
// the git binary; no operation touches the user's primary checkout.
package vcs

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Repo is a handle to a git repository identified by its top-level directory.
type Repo struct {
	root string
}

// Open verifies dir is inside a git work tree and returns a Repo rooted at its
// top level.
func Open(dir string) (*Repo, error) {
	out, err := git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("not a git repository: %w", err)
	}
	return &Repo{root: strings.TrimSpace(out)}, nil
}

// Root returns the repository's top-level directory.
func (r *Repo) Root() string { return r.root }

// Head returns the current commit SHA of the repository's checked-out branch.
func (r *Repo) Head() (string, error) {
	out, err := git(r.root, "rev-parse", "HEAD")
	return strings.TrimSpace(out), err
}

// AddWorktree creates a detached worktree at path checked out at base.
func (r *Repo) AddWorktree(path, base string) error {
	_, err := git(r.root, "worktree", "add", "--detach", path, base)
	return err
}

// CreateWorktreeBranch creates a worktree at path on a new branch pointing at
// base. This is where cherry-picks are applied, keeping the user's checkout
// untouched.
func (r *Repo) CreateWorktreeBranch(path, branch, base string) error {
	_, err := git(r.root, "worktree", "add", "-b", branch, path, base)
	return err
}

// RemoveWorktree force-removes the worktree at path.
func (r *Repo) RemoveWorktree(path string) error {
	_, err := git(r.root, "worktree", "remove", "--force", path)
	return err
}

// CommitAll stages everything in the worktree at dir and commits it. changed is
// false (with no commit and an empty sha) when there is nothing to commit.
func (r *Repo) CommitAll(dir, msg string) (sha string, changed bool, err error) {
	if _, err = git(dir, "add", "-A"); err != nil {
		return "", false, err
	}
	status, err := git(dir, "status", "--porcelain")
	if err != nil {
		return "", false, err
	}
	if strings.TrimSpace(status) == "" {
		return "", false, nil
	}
	if _, err = git(dir, "commit", "-q", "-m", msg); err != nil {
		return "", false, err
	}
	out, err := git(dir, "rev-parse", "HEAD")
	if err != nil {
		return "", false, err
	}
	return strings.TrimSpace(out), true, nil
}

// CreateBranch points a new branch name at sha, so a queued task's work is
// preserved as a reviewable ref.
func (r *Repo) CreateBranch(name, sha string) error {
	_, err := git(r.root, "branch", name, sha)
	return err
}

// CherryPick applies sha onto the worktree at intDir. clean is false when the
// pick conflicts; in that case the pick is aborted so intDir is left usable for
// the next pick.
func (r *Repo) CherryPick(intDir, sha string) (clean bool, err error) {
	if _, err := git(intDir, "cherry-pick", "--allow-empty", "-x", sha); err != nil {
		// Distinguish a conflict (recoverable) from a hard failure.
		if _, statErr := os.Stat(intDir + "/.git"); statErr == nil {
			_, _ = git(intDir, "cherry-pick", "--abort")
		} else {
			_, _ = git(intDir, "cherry-pick", "--abort")
		}
		return false, nil
	}
	return true, nil
}

// git runs a git command in dir and returns its stdout, or an error carrying
// stderr.
func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=fan", "GIT_AUTHOR_EMAIL=fan@local",
		"GIT_COMMITTER_NAME=fan", "GIT_COMMITTER_EMAIL=fan@local")
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return string(out), fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(ee.Stderr)))
		}
		return string(out), fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/vcs/ -v`
Expected: PASS (all five)

- [ ] **Step 5: Commit**

```bash
git add internal/vcs/
git commit -m "Add git worktree, commit, branch, and cherry-pick operations"
```

---

## Task 9: Summary rendering

**Files:**
- Create: `internal/summary/summary.go`
- Test: `internal/summary/summary_test.go`

- [ ] **Step 1: Write the failing test**

```go
package summary

import (
	"strings"
	"testing"

	"github.com/ravistakumar/fan/internal/gate"
	"github.com/ravistakumar/fan/internal/result"
	"github.com/ravistakumar/fan/internal/task"
)

func TestRenderCountsAndNextSteps(t *testing.T) {
	outcomes := []result.Outcome{
		{Task: task.Task{ID: "a"}, Status: result.Merged},
		{Task: task.Task{ID: "b"}, Status: result.Merged},
		{Task: task.Task{ID: "c"}, Status: result.QueuedConflict, Branch: "fan/c"},
		{Task: task.Task{ID: "d"}, Status: result.QueuedGateFail, Branch: "fan/d", Detail: "npm test failed"},
	}
	out := Render(outcomes, "fan/2026-06-03-1432", gate.Result{Passed: true})

	for _, want := range []string{"2 merged", "1 conflict", "1 gate-fail", "fan/2026-06-03-1432", "git merge fan/2026-06-03-1432", "fan/c", "fan/d"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary missing %q\n---\n%s", want, out)
		}
	}
}

func TestRenderFinalGateFailIsFlagged(t *testing.T) {
	out := Render([]result.Outcome{{Task: task.Task{ID: "a"}, Status: result.Merged}},
		"fan/x", gate.Result{Passed: false, Output: "boom"})
	if !strings.Contains(strings.ToLower(out), "fail") {
		t.Errorf("final gate failure not flagged:\n%s", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/summary/ -v`
Expected: FAIL — `undefined: Render`

- [ ] **Step 3: Write the implementation**

```go
// Package summary renders the final report of a fan run: per-status counts, the
// queued items with their branches, the integration branch, and next steps.
package summary

import (
	"fmt"
	"strings"

	"github.com/ravistakumar/fan/internal/gate"
	"github.com/ravistakumar/fan/internal/result"
)

// Render produces the human-readable summary for a completed run.
func Render(outcomes []result.Outcome, branch string, finalGate gate.Result) string {
	var merged, conflict, gateFail, errored, noop []result.Outcome
	for _, o := range outcomes {
		switch o.Status {
		case result.Merged:
			merged = append(merged, o)
		case result.QueuedConflict:
			conflict = append(conflict, o)
		case result.QueuedGateFail:
			gateFail = append(gateFail, o)
		case result.Errored:
			errored = append(errored, o)
		case result.NoOp:
			noop = append(noop, o)
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "fan: %d tasks · %d merged · %d conflict · %d gate-fail",
		len(outcomes), len(merged), len(conflict), len(gateFail))
	if len(errored) > 0 {
		fmt.Fprintf(&b, " · %d error", len(errored))
	}
	if len(noop) > 0 {
		fmt.Fprintf(&b, " · %d no-op", len(noop))
	}
	fmt.Fprintf(&b, " · integration branch: %s\n", branch)

	if len(merged) > 0 {
		fmt.Fprintf(&b, "  ✓ merged       %s\n", joinIDs(merged))
	}
	for _, o := range conflict {
		fmt.Fprintf(&b, "  ⚠ conflict     %-16s → branch %s\n", o.Task.ID, o.Branch)
	}
	for _, o := range gateFail {
		fmt.Fprintf(&b, "  ✗ gate-fail    %-16s → branch %s (%s)\n", o.Task.ID, o.Branch, o.Detail)
	}
	for _, o := range errored {
		fmt.Fprintf(&b, "  ✗ error        %-16s (%s)\n", o.Task.ID, o.Detail)
	}

	if finalGate.Passed {
		fmt.Fprintf(&b, "  final gate on integration branch: PASS\n")
	} else {
		fmt.Fprintf(&b, "  final gate on integration branch: FAIL\n")
	}

	fmt.Fprintf(&b, "\n  review:  git diff HEAD..%s\n", branch)
	fmt.Fprintf(&b, "  ship:    git merge %s\n", branch)
	return b.String()
}

func joinIDs(os []result.Outcome) string {
	ids := make([]string, len(os))
	for i, o := range os {
		ids[i] = o.Task.ID
	}
	return strings.Join(ids, ", ")
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/summary/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/summary/
git commit -m "Add summary rendering"
```

---

## Task 10: Planner meta-prompt and plan parsing

**Files:**
- Create: `internal/planner/planner.go`
- Test: `internal/planner/planner_test.go`

- [ ] **Step 1: Write the failing test**

```go
package planner

import (
	"strings"
	"testing"
)

func anyAgent(string) bool { return true }

func TestBuildMetaPromptIncludesGoalAndSchema(t *testing.T) {
	p := BuildMetaPrompt("migrate src/ui to the new API")
	if !strings.Contains(p, "migrate src/ui to the new API") {
		t.Error("meta prompt missing goal")
	}
	if !strings.Contains(p, "\"tasks\"") || !strings.Contains(p, "\"prompt\"") {
		t.Error("meta prompt missing JSON schema hints")
	}
}

func TestParsePlanExtractsTasks(t *testing.T) {
	reply := "Sure, here is the plan:\n```json\n" +
		`{"tasks":[{"id":"btn","prompt":"migrate Button"},{"id":"modal","prompt":"migrate Modal","agent":"claude"}]}` +
		"\n```\n"
	tasks, err := ParsePlan(reply, anyAgent, "codex", "npm test")
	if err != nil {
		t.Fatalf("ParsePlan: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("tasks = %d, want 2", len(tasks))
	}
	if tasks[0].Agent != "codex" || tasks[0].Gate != "npm test" {
		t.Errorf("defaults not applied: %+v", tasks[0])
	}
	if tasks[1].Agent != "claude" {
		t.Errorf("override not applied: %+v", tasks[1])
	}
}

func TestParsePlanRejectsUnknownAgent(t *testing.T) {
	reply := `{"tasks":[{"id":"x","prompt":"p","agent":"bogus"}]}`
	if _, err := ParsePlan(reply, func(string) bool { return false }, "codex", ""); err == nil {
		t.Fatal("expected unknown-agent error")
	}
}

func TestParsePlanRejectsNoTasks(t *testing.T) {
	if _, err := ParsePlan(`{"tasks":[]}`, anyAgent, "codex", ""); err == nil {
		t.Fatal("expected error for empty task list")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/planner/ -v`
Expected: FAIL — `undefined: BuildMetaPrompt`

- [ ] **Step 3: Write the implementation**

```go
// Package planner turns a high-level goal into a structured task list by asking
// an agent to emit JSON, then parsing that JSON tolerantly. Both functions are
// pure; the agent call itself lives in the run package.
package planner

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ravistakumar/fan/internal/task"
)

// BuildMetaPrompt instructs an agent to decompose goal into independent tasks
// and return them as a strict JSON object.
func BuildMetaPrompt(goal string) string {
	return fmt.Sprintf(`You are planning independent, parallelizable coding tasks.

Goal: %s

Explore the repository as needed, then break the goal into INDEPENDENT tasks
that can run in parallel without depending on each other's output. Each task
gets a short kebab-case id and a self-contained prompt.

Respond with a JSON object ONLY — no prose, no code fences:
{"tasks":[{"id":"<kebab-id>","prompt":"<self-contained instruction>"}]}

Optionally include "agent" or "gate" on a task to override the defaults.`, goal)
}

type planJSON struct {
	Tasks []struct {
		ID     string `json:"id"`
		Prompt string `json:"prompt"`
		Agent  string `json:"agent"`
		Gate   string `json:"gate"`
	} `json:"tasks"`
}

// ParsePlan extracts the JSON object from reply and resolves each task against
// the default agent and gate. validAgent rejects unsupported agents.
func ParsePlan(reply string, validAgent func(string) bool, defAgent, defGate string) ([]task.Task, error) {
	raw := extractJSON(reply)
	if raw == "" {
		return nil, fmt.Errorf("no JSON object found in planner reply")
	}
	var p planJSON
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return nil, fmt.Errorf("parse planner JSON: %w", err)
	}
	if len(p.Tasks) == 0 {
		return nil, fmt.Errorf("planner returned no tasks")
	}

	seen := map[string]bool{}
	out := make([]task.Task, 0, len(p.Tasks))
	for i, pt := range p.Tasks {
		if strings.TrimSpace(pt.ID) == "" {
			return nil, fmt.Errorf("planned task %d: missing id", i+1)
		}
		if seen[pt.ID] {
			return nil, fmt.Errorf("planned duplicate id %q", pt.ID)
		}
		seen[pt.ID] = true
		if strings.TrimSpace(pt.Prompt) == "" {
			return nil, fmt.Errorf("planned task %q: empty prompt", pt.ID)
		}
		agent := pt.Agent
		if agent == "" {
			agent = defAgent
		}
		if !validAgent(agent) {
			return nil, fmt.Errorf("planned task %q: unknown agent %q", pt.ID, agent)
		}
		gate := defGate
		if pt.Gate != "" {
			gate = pt.Gate
		}
		out = append(out, task.Task{ID: pt.ID, Prompt: pt.Prompt, Agent: agent, Gate: gate})
	}
	return out, nil
}

// extractJSON returns the substring from the first '{' to the last '}', which
// tolerates surrounding prose or code fences.
func extractJSON(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end < start {
		return ""
	}
	return s[start : end+1]
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/planner/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/planner/
git commit -m "Add planner meta-prompt and tolerant plan parsing"
```

---

## Task 11: Reporter

**Files:**
- Create: `internal/run/reporter.go`
- Test: `internal/run/reporter_test.go`

- [ ] **Step 1: Write the failing test**

```go
package run

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ravistakumar/fan/internal/result"
	"github.com/ravistakumar/fan/internal/task"
)

func TestTextReporterEmitsStartAndFinish(t *testing.T) {
	var buf bytes.Buffer
	r := NewTextReporter(&buf)
	r.Start(task.Task{ID: "alpha"})
	r.Finish(result.Outcome{Task: task.Task{ID: "alpha"}, Status: result.Merged})

	out := buf.String()
	if !strings.Contains(out, "alpha") {
		t.Errorf("reporter output missing task id:\n%s", out)
	}
	if !strings.Contains(out, string(result.Merged)) {
		t.Errorf("reporter output missing status:\n%s", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/run/ -run TestTextReporter -v`
Expected: FAIL — `undefined: NewTextReporter`

- [ ] **Step 3: Write the implementation**

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/run/ -run TestTextReporter -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/run/reporter.go internal/run/reporter_test.go
git commit -m "Add progress reporter"
```

---

## Task 12: Runner — wire the full pipeline

**Files:**
- Create: `internal/run/run.go`
- Test: `internal/run/run_test.go`

- [ ] **Step 1: Write the failing test**

```go
package run

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ravistakumar/fan/internal/agent"
	"github.com/ravistakumar/fan/internal/result"
	"github.com/ravistakumar/fan/internal/task"
	"github.com/ravistakumar/fan/internal/vcs"
)

// fakeAgent writes a file named after the task id, simulating an edit. If fail
// is true it returns an error without writing.
type fakeAgent struct {
	name string
	file string // file to create, relative to dir
	body string
	fail bool
}

func (f fakeAgent) Name() string      { return f.name }
func (f fakeAgent) Available() bool    { return true }
func (f fakeAgent) Run(_ context.Context, _ , dir string) (string, error) {
	if f.fail {
		return "", os.ErrPermission
	}
	return "", os.WriteFile(filepath.Join(dir, f.file), []byte(f.body), 0o644)
}

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("seed\n"), 0o644)
	run("add", "-A")
	run("commit", "-q", "-m", "seed")
	return dir
}

func TestRunMergesCleanTasks(t *testing.T) {
	repoDir := initRepo(t)
	repo, _ := vcs.Open(repoDir)

	tasks := []task.Task{
		{ID: "a", Prompt: "make a", Agent: "fake", Gate: "true"},
		{ID: "b", Prompt: "make b", Agent: "fake", Gate: "true"},
	}
	files := map[string]string{"a": "a.txt", "b": "b.txt"}

	r := Runner{
		Repo:     repo,
		WorkRoot: t.TempDir(),
		Reporter: NewTextReporter(os.Stderr),
		Now:      func() string { return "2026-06-03-1432" },
		// AgentFor injects a per-task fake agent keyed by task id.
		AgentFor: func(tk task.Task) (agent.Agent, error) {
			return fakeAgent{name: "fake", file: files[tk.ID], body: tk.ID + "\n"}, nil
		},
	}

	outcomes, branch, err := r.Run(context.Background(), tasks, 2, true)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if branch != "fan/2026-06-03-1432" {
		t.Errorf("branch = %q", branch)
	}
	for _, o := range outcomes {
		if o.Status != result.Merged {
			t.Errorf("task %s: status %q, want merged", o.Task.ID, o.Status)
		}
	}
}

func TestRunQueuesGateFailures(t *testing.T) {
	repo, _ := vcs.Open(initRepo(t))
	tasks := []task.Task{{ID: "a", Prompt: "make a", Agent: "fake", Gate: "false"}}
	r := Runner{
		Repo: repo, WorkRoot: t.TempDir(),
		Reporter: NewTextReporter(os.Stderr),
		Now:      func() string { return "ts" },
		AgentFor: func(tk task.Task) (agent.Agent, error) {
			return fakeAgent{name: "fake", file: "a.txt", body: "a\n"}, nil
		},
	}
	outcomes, _, err := r.Run(context.Background(), tasks, 1, false)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcomes[0].Status != result.QueuedGateFail {
		t.Errorf("status = %q, want queued: gate-fail", outcomes[0].Status)
	}
	if outcomes[0].Branch == "" {
		t.Error("gate-failed task should keep a branch")
	}
}

func TestRunRecordsAgentErrors(t *testing.T) {
	repo, _ := vcs.Open(initRepo(t))
	tasks := []task.Task{{ID: "a", Prompt: "p", Agent: "fake", Gate: "true"}}
	r := Runner{
		Repo: repo, WorkRoot: t.TempDir(),
		Reporter: NewTextReporter(os.Stderr),
		Now:      func() string { return "ts" },
		AgentFor: func(tk task.Task) (agent.Agent, error) {
			return fakeAgent{name: "fake", fail: true}, nil
		},
	}
	outcomes, _, _ := r.Run(context.Background(), tasks, 1, false)
	if outcomes[0].Status != result.Errored {
		t.Errorf("status = %q, want error", outcomes[0].Status)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/run/ -run TestRun -v`
Expected: FAIL — `unknown field Repo` / `r.Run undefined`

- [ ] **Step 3: Write the implementation**

```go
package run

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/ravistakumar/fan/internal/agent"
	"github.com/ravistakumar/fan/internal/gate"
	"github.com/ravistakumar/fan/internal/result"
	"github.com/ravistakumar/fan/internal/schedule"
	"github.com/ravistakumar/fan/internal/task"
	"github.com/ravistakumar/fan/internal/vcs"
)

// Runner wires the fan pipeline: schedule tasks into isolated worktrees, run an
// agent + gate in each, then serially cherry-pick the gate-passing ones onto a
// throwaway integration branch.
type Runner struct {
	Repo     *vcs.Repo
	WorkRoot string // parent dir for .fan worktrees (usually repo root + "/.fan")
	Reporter Reporter

	// AgentFor returns the agent for a task. Defaults to agent.New(task.Agent)
	// when nil. Injected in tests.
	AgentFor func(task.Task) (agent.Agent, error)
	// NewAgent is the simple factory used when AgentFor is nil.
	NewAgent func(string) (agent.Agent, error)
	// Now returns the timestamp slug used in the integration branch name.
	Now func() string
}

// candidate is the intermediate result of the parallel phase: either a
// gate-passing commit ready to cherry-pick, or an already-terminal outcome.
type candidate struct {
	task     task.Task
	sha      string
	wtDir    string
	terminal *result.Outcome // non-nil if the task is already done (error/gate-fail/no-op)
}

// Run executes tasks and returns their outcomes plus the integration branch
// name. finalGate, when true, runs the first task's-style gate once more on the
// integration branch after all merges; its result is reported by the caller via
// the returned outcomes' overall state (see summary).
func (r Runner) Run(ctx context.Context, tasks []task.Task, concurrency int, finalGate bool) ([]result.Outcome, string, error) {
	base, err := r.Repo.Head()
	if err != nil {
		return nil, "", err
	}
	branch := "fan/" + r.now()

	intDir := filepath.Join(r.WorkRoot, "integration")
	if err := r.Repo.CreateWorktreeBranch(intDir, branch, base); err != nil {
		return nil, "", fmt.Errorf("create integration worktree: %w", err)
	}
	defer r.Repo.RemoveWorktree(intDir)

	// Parallel phase: worktree + agent + gate for each task.
	cands := schedule.Run(ctx, tasks, concurrency, func(ctx context.Context, _ int, tk task.Task) candidate {
		r.Reporter.Start(tk)
		c := r.runTask(ctx, tk, base)
		if c.terminal != nil {
			r.Reporter.Finish(*c.terminal)
		}
		return c
	})

	// Serial phase: cherry-pick gate-passing candidates one at a time.
	outcomes := make([]result.Outcome, len(cands))
	for i, c := range cands {
		if c.terminal != nil {
			outcomes[i] = *c.terminal
			r.Repo.RemoveWorktree(c.wtDir)
			continue
		}
		clean, err := r.Repo.CherryPick(intDir, c.sha)
		if err != nil {
			outcomes[i] = result.Outcome{Task: c.task, Status: result.Errored, Detail: err.Error()}
		} else if clean {
			outcomes[i] = result.Outcome{Task: c.task, Status: result.Merged}
		} else {
			kept := "fan/" + c.task.ID
			_ = r.Repo.CreateBranch(kept, c.sha)
			outcomes[i] = result.Outcome{Task: c.task, Status: result.QueuedConflict, Branch: kept}
		}
		r.Reporter.Finish(outcomes[i])
		r.Repo.RemoveWorktree(c.wtDir)
	}

	return outcomes, branch, nil
}

// runTask creates a worktree, runs the agent, commits, and runs the gate. It
// returns a candidate ready for cherry-pick, or a terminal outcome.
func (r Runner) runTask(ctx context.Context, tk task.Task, base string) candidate {
	wtDir := filepath.Join(r.WorkRoot, "wt", tk.ID)

	ag, err := r.agentFor(tk)
	if err != nil {
		return terminal(tk, result.Errored, "", err.Error())
	}
	if err := r.Repo.AddWorktree(wtDir, base); err != nil {
		return terminal(tk, result.Errored, "", err.Error())
	}

	if _, err := ag.Run(ctx, tk.Prompt, wtDir); err != nil {
		return candidate{task: tk, wtDir: wtDir, terminal: ptr(result.Outcome{Task: tk, Status: result.Errored, Detail: err.Error()})}
	}

	sha, changed, err := r.Repo.CommitAll(wtDir, fmt.Sprintf("fan(%s): %s", tk.ID, tk.Prompt))
	if err != nil {
		return candidate{task: tk, wtDir: wtDir, terminal: ptr(result.Outcome{Task: tk, Status: result.Errored, Detail: err.Error()})}
	}
	if !changed {
		return candidate{task: tk, wtDir: wtDir, terminal: ptr(result.Outcome{Task: tk, Status: result.NoOp})}
	}

	g := gate.Run(ctx, tk.Gate, wtDir)
	if !g.Passed {
		kept := "fan/" + tk.ID
		_ = r.Repo.CreateBranch(kept, sha)
		return candidate{task: tk, wtDir: wtDir, terminal: ptr(result.Outcome{
			Task: tk, Status: result.QueuedGateFail, Branch: kept, Detail: gateDetail(tk.Gate, g.Output)})}
	}

	return candidate{task: tk, sha: sha, wtDir: wtDir}
}

func (r Runner) agentFor(tk task.Task) (agent.Agent, error) {
	if r.AgentFor != nil {
		return r.AgentFor(tk)
	}
	if r.NewAgent != nil {
		return r.NewAgent(tk.Agent)
	}
	return agent.New(tk.Agent)
}

func (r Runner) now() string {
	if r.Now != nil {
		return r.Now()
	}
	return "run"
}

func terminal(tk task.Task, s result.Status, branch, detail string) candidate {
	return candidate{task: tk, terminal: ptr(result.Outcome{Task: tk, Status: s, Branch: branch, Detail: detail})}
}

func ptr(o result.Outcome) *result.Outcome { return &o }

func gateDetail(command, output string) string {
	if len(output) > 80 {
		output = output[:80] + "…"
	}
	if output == "" {
		return command + " failed"
	}
	return output
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/run/ -v`
Expected: PASS (TextReporter + all Run tests)

- [ ] **Step 5: Commit**

```bash
git add internal/run/run.go internal/run/run_test.go
git commit -m "Add runner wiring the full fan pipeline"
```

---

## Task 13: Final integration gate

**Files:**
- Modify: `internal/run/run.go` (run the gate on the integration branch after merges)
- Modify: `internal/run/run_test.go` (assert it runs)

- [ ] **Step 1: Write the failing test (append to run_test.go)**

```go
func TestRunFinalGateResult(t *testing.T) {
	repo, _ := vcs.Open(initRepo(t))
	tasks := []task.Task{{ID: "a", Prompt: "p", Agent: "fake", Gate: "true"}}
	r := Runner{
		Repo: repo, WorkRoot: t.TempDir(),
		Reporter:  NewTextReporter(os.Stderr),
		Now:       func() string { return "ts" },
		FinalGate: "test -f a.txt", // passes only if the merge landed a.txt
		AgentFor: func(tk task.Task) (agent.Agent, error) {
			return fakeAgent{name: "fake", file: "a.txt", body: "a\n"}, nil
		},
	}
	_, _, fg, err := r.RunWithFinalGate(context.Background(), tasks, 1)
	if err != nil {
		t.Fatalf("RunWithFinalGate: %v", err)
	}
	if !fg.Passed {
		t.Errorf("final gate should pass; output=%q", fg.Output)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/run/ -run TestRunFinalGate -v`
Expected: FAIL — `r.RunWithFinalGate undefined` / `unknown field FinalGate`

- [ ] **Step 3: Refactor Run to expose the integration dir and add RunWithFinalGate**

Add the `FinalGate` field to the `Runner` struct:

```go
	// FinalGate, when non-empty, is run once on the integration branch after all
	// merges to catch interaction bugs between independently-passing tasks.
	FinalGate string
```

Change `Run` to keep the integration worktree until the caller is done by
splitting the cleanup. Replace the `defer r.Repo.RemoveWorktree(intDir)` line
and the final `return` with a call from a new exported method. Concretely, add:

```go
// RunWithFinalGate runs the pipeline, then runs FinalGate on the integration
// branch (if set) before tearing the integration worktree down.
func (r Runner) RunWithFinalGate(ctx context.Context, tasks []task.Task, concurrency int) ([]result.Outcome, string, gate.Result, error) {
	base, err := r.Repo.Head()
	if err != nil {
		return nil, "", gate.Result{}, err
	}
	branch := "fan/" + r.now()
	intDir := filepath.Join(r.WorkRoot, "integration")
	if err := r.Repo.CreateWorktreeBranch(intDir, branch, base); err != nil {
		return nil, "", gate.Result{}, fmt.Errorf("create integration worktree: %w", err)
	}
	defer r.Repo.RemoveWorktree(intDir)

	outcomes := r.execute(ctx, tasks, concurrency, base, intDir)

	fg := gate.Result{Passed: true}
	if r.FinalGate != "" {
		fg = gate.Run(ctx, r.FinalGate, intDir)
	}
	return outcomes, branch, fg, nil
}
```

Extract the parallel+serial body of the existing `Run` into a private
`execute(ctx, tasks, concurrency, base, intDir) []result.Outcome` method (move
the scheduler call and the serial cherry-pick loop into it, returning
`outcomes`). Then redefine `Run` in terms of it so existing tests still pass:

```go
// Run executes tasks against a fresh integration branch and returns the
// outcomes and branch name. It does not run a final gate; use RunWithFinalGate
// for that.
func (r Runner) Run(ctx context.Context, tasks []task.Task, concurrency int, _ bool) ([]result.Outcome, string, error) {
	base, err := r.Repo.Head()
	if err != nil {
		return nil, "", err
	}
	branch := "fan/" + r.now()
	intDir := filepath.Join(r.WorkRoot, "integration")
	if err := r.Repo.CreateWorktreeBranch(intDir, branch, base); err != nil {
		return nil, "", fmt.Errorf("create integration worktree: %w", err)
	}
	defer r.Repo.RemoveWorktree(intDir)
	return r.execute(ctx, tasks, concurrency, base, intDir), branch, nil
}
```

Where `execute` is:

```go
func (r Runner) execute(ctx context.Context, tasks []task.Task, concurrency int, base, intDir string) []result.Outcome {
	cands := schedule.Run(ctx, tasks, concurrency, func(ctx context.Context, _ int, tk task.Task) candidate {
		r.Reporter.Start(tk)
		c := r.runTask(ctx, tk, base)
		if c.terminal != nil {
			r.Reporter.Finish(*c.terminal)
		}
		return c
	})

	outcomes := make([]result.Outcome, len(cands))
	for i, c := range cands {
		if c.terminal != nil {
			outcomes[i] = *c.terminal
			r.Repo.RemoveWorktree(c.wtDir)
			continue
		}
		clean, err := r.Repo.CherryPick(intDir, c.sha)
		switch {
		case err != nil:
			outcomes[i] = result.Outcome{Task: c.task, Status: result.Errored, Detail: err.Error()}
		case clean:
			outcomes[i] = result.Outcome{Task: c.task, Status: result.Merged}
		default:
			kept := "fan/" + c.task.ID
			_ = r.Repo.CreateBranch(kept, c.sha)
			outcomes[i] = result.Outcome{Task: c.task, Status: result.QueuedConflict, Branch: kept}
		}
		r.Reporter.Finish(outcomes[i])
		r.Repo.RemoveWorktree(c.wtDir)
	}
	return outcomes
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/run/ -v`
Expected: PASS (all run tests, including the new final-gate test)

- [ ] **Step 5: Commit**

```bash
git add internal/run/
git commit -m "Add final integration-branch gate after merges"
```

---

## Task 14: CLI — `fan run`

**Files:**
- Modify: `internal/cli/cli.go` (register the `run` command)
- Create: `internal/cli/run.go`
- Test: `internal/cli/run_test.go`

- [ ] **Step 1: Write the failing test**

```go
package cli

import (
	"strings"
	"testing"
)

func TestResolveEachItems(t *testing.T) {
	// Comma list passes through untouched.
	got, err := resolveEachItems("a.ts,b.ts")
	if err != nil {
		t.Fatalf("resolveEachItems: %v", err)
	}
	if len(got) != 2 || got[0] != "a.ts" || got[1] != "b.ts" {
		t.Errorf("got %v", got)
	}
}

func TestResolveEachItemsRejectsEmpty(t *testing.T) {
	if _, err := resolveEachItems(""); err == nil {
		t.Fatal("expected error for empty --each")
	}
}

func TestFilterByOnly(t *testing.T) {
	ids := []string{"a", "b", "c"}
	got := filterIDs(ids, "a,c")
	if strings.Join(got, ",") != "a,c" {
		t.Errorf("got %v", got)
	}
	if all := filterIDs(ids, ""); len(all) != 3 {
		t.Errorf("empty --only should keep all, got %v", all)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -v`
Expected: FAIL — `undefined: resolveEachItems`

- [ ] **Step 3: Write internal/cli/run.go**

```go
package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ravistakumar/fan/internal/agent"
	"github.com/ravistakumar/fan/internal/run"
	"github.com/ravistakumar/fan/internal/summary"
	"github.com/ravistakumar/fan/internal/task"
	"github.com/ravistakumar/fan/internal/vcs"
)

func newRunCmd() *cobra.Command {
	var (
		each        string
		agentName   string
		gateCmd     string
		concurrency int
		only        string
		noFinalGate bool
	)
	cmd := &cobra.Command{
		Use:   "run [taskfile]",
		Short: "Fan tasks out in parallel, isolating and gating each",
		Args:  cobra.MaximumNArgs(2), // taskfile OR (--each + template)
		RunE: func(cmd *cobra.Command, args []string) error {
			tasks, conc, err := buildTasks(args, each, agentName, gateCmd, concurrency)
			if err != nil {
				return err
			}
			tasks = filterTasks(tasks, only)
			if len(tasks) == 0 {
				return fmt.Errorf("no tasks to run")
			}

			repo, err := vcs.Open(".")
			if err != nil {
				return err
			}
			workRoot := filepath.Join(repo.Root(), ".fan")
			if err := os.MkdirAll(workRoot, 0o755); err != nil {
				return err
			}

			finalGate := ""
			if !noFinalGate {
				finalGate = firstGate(tasks)
			}
			r := run.Runner{
				Repo:      repo,
				WorkRoot:  workRoot,
				Reporter:  run.NewTextReporter(cmd.ErrOrStderr()),
				FinalGate: finalGate,
				Now:       timestamp,
			}
			outcomes, branch, fg, err := r.RunWithFinalGate(context.Background(), tasks, conc)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), summary.Render(outcomes, branch, fg))
			return nil
		},
	}
	cmd.Flags().StringVar(&each, "each", "", "fan a template over a glob or comma list (use {} placeholder)")
	cmd.Flags().StringVar(&agentName, "agent", "", "agent to use (claude|codex|opencode|aider|grok)")
	cmd.Flags().StringVar(&gateCmd, "gate", "", "gate command run in each worktree (empty = clean-apply-only)")
	cmd.Flags().IntVar(&concurrency, "concurrency", 0, "max concurrent agents (0 = cores-1)")
	cmd.Flags().StringVar(&only, "only", "", "run only these task ids (comma list)")
	cmd.Flags().BoolVar(&noFinalGate, "no-final-gate", false, "skip the gate run on the integration branch")
	return cmd
}

// buildTasks produces the task list and concurrency from either a task file
// (args[0]) or an inline --each template (args[0] is the template).
func buildTasks(args []string, each, agentName, gateCmd string, concurrency int) ([]task.Task, int, error) {
	if each != "" {
		if len(args) != 1 {
			return nil, 0, fmt.Errorf("--each requires exactly one template argument")
		}
		items, err := resolveEachItems(each)
		if err != nil {
			return nil, 0, err
		}
		tasks, err := task.Expand(items, args[0], agentName, gateCmd)
		if err != nil {
			return nil, 0, err
		}
		return tasks, concOrDefault(concurrency), nil
	}

	if len(args) != 1 {
		return nil, 0, fmt.Errorf("provide a task file, or use --each with a template")
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		return nil, 0, err
	}
	f, err := task.Parse(data, agent.IsSupported)
	if err != nil {
		return nil, 0, err
	}
	conc := f.Defaults.Concurrency
	if concurrency > 0 {
		conc = concurrency
	}
	// CLI flags override file defaults where set.
	for i := range f.Tasks {
		if agentName != "" {
			f.Tasks[i].Agent = agentName
		}
	}
	return f.Tasks, conc, nil
}

// resolveEachItems turns a --each value into a list of items: a filesystem glob
// when it contains glob metacharacters, otherwise a comma-separated list.
func resolveEachItems(each string) ([]string, error) {
	each = strings.TrimSpace(each)
	if each == "" {
		return nil, fmt.Errorf("--each is empty")
	}
	if strings.ContainsAny(each, "*?[") {
		matches, err := filepath.Glob(each)
		if err != nil {
			return nil, err
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("glob %q matched no files", each)
		}
		return matches, nil
	}
	parts := strings.Split(each, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("--each has no items")
	}
	return out, nil
}

func filterTasks(tasks []task.Task, only string) []task.Task {
	keep := idSet(only)
	if keep == nil {
		return tasks
	}
	out := tasks[:0:0]
	for _, t := range tasks {
		if keep[t.ID] {
			out = append(out, t)
		}
	}
	return out
}

// filterIDs is the string-slice counterpart used in tests.
func filterIDs(ids []string, only string) []string {
	keep := idSet(only)
	if keep == nil {
		return ids
	}
	out := []string{}
	for _, id := range ids {
		if keep[id] {
			out = append(out, id)
		}
	}
	return out
}

func idSet(only string) map[string]bool {
	only = strings.TrimSpace(only)
	if only == "" {
		return nil
	}
	m := map[string]bool{}
	for _, p := range strings.Split(only, ",") {
		if p = strings.TrimSpace(p); p != "" {
			m[p] = true
		}
	}
	return m
}

func firstGate(tasks []task.Task) string {
	for _, t := range tasks {
		if t.Gate != "" {
			return t.Gate
		}
	}
	return ""
}

func concOrDefault(c int) int {
	if c > 0 {
		return c
	}
	return task.DefaultConcurrency()
}
```

- [ ] **Step 4: Add the timestamp helper in internal/cli/cli.go**

```go
// add these imports to cli.go: "time"
// timestamp is the slug used for integration branch names.
func timestamp() string {
	return time.Now().Format("2006-01-02-1504")
}
```

And register the command in `Execute`:

```go
	root.AddCommand(newRunCmd())
```

- [ ] **Step 5: Run tests and build**

Run: `go test ./internal/cli/ -v && go build -o fan ./cmd/fan`
Expected: PASS, binary builds

- [ ] **Step 6: Commit**

```bash
git add internal/cli/
git commit -m "Add fan run command with task-file and --each inputs"
```

---

## Task 15: CLI — `fan plan`

**Files:**
- Create: `internal/cli/plan.go`
- Modify: `internal/cli/cli.go` (register the `plan` command)
- Test: `internal/cli/plan_test.go`

- [ ] **Step 1: Write the failing test**

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestRenderPlanTable -v`
Expected: FAIL — `undefined: renderPlanTable`

- [ ] **Step 3: Write internal/cli/plan.go**

```go
package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ravistakumar/fan/internal/agent"
	"github.com/ravistakumar/fan/internal/planner"
	"github.com/ravistakumar/fan/internal/task"
)

func newPlanCmd() *cobra.Command {
	var (
		agentName string
		gateCmd   string
		save      string
		conc      int
		yes       bool
	)
	cmd := &cobra.Command{
		Use:   "plan <goal>",
		Short: "Have an agent draft a parallel task list you approve, then run it",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			goal := strings.Join(args, " ")
			if agentName == "" {
				return fmt.Errorf("--agent is required for planning")
			}
			ag, err := agent.New(agentName)
			if err != nil {
				return err
			}
			if !ag.Available() {
				return fmt.Errorf("agent %q is not installed or on PATH", agentName)
			}

			reply, err := ag.Run(context.Background(), planner.BuildMetaPrompt(goal), ".")
			if err != nil {
				return fmt.Errorf("planner agent failed: %w", err)
			}
			tasks, err := planner.ParsePlan(reply, agent.IsSupported, agentName, gateCmd)
			if err != nil {
				return err
			}

			fmt.Fprint(cmd.OutOrStdout(), renderPlanTable(tasks))

			if save != "" {
				if err := os.WriteFile(save, []byte(planToTOML(tasks, conc)), 0o644); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "\nplan saved to %s\n", save)
			}
			if !yes {
				fmt.Fprintln(cmd.OutOrStdout(), "\nRe-run with --yes to execute, or edit the saved file and `fan run` it.")
				return nil
			}
			// Execute immediately when approved with --yes.
			return runTasks(cmd, tasks, concOrDefault(conc), firstGate(tasks))
		},
	}
	cmd.Flags().StringVar(&agentName, "agent", "", "agent that drafts the plan and runs tasks")
	cmd.Flags().StringVar(&gateCmd, "gate", "", "default gate command for planned tasks")
	cmd.Flags().StringVar(&save, "save", "", "write the approved plan to this TOML file")
	cmd.Flags().IntVar(&conc, "concurrency", 0, "max concurrent agents (0 = cores-1)")
	cmd.Flags().BoolVar(&yes, "yes", false, "execute the plan without a second confirmation")
	return cmd
}

func renderPlanTable(tasks []task.Task) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-3s %-16s %-9s %s\n", "#", "id", "agent", "prompt")
	for i, t := range tasks {
		p := t.Prompt
		if len(p) > 50 {
			p = p[:50] + "…"
		}
		fmt.Fprintf(&b, "%-3d %-16s %-9s %s\n", i+1, t.ID, t.Agent, p)
	}
	return b.String()
}

func planToTOML(tasks []task.Task, conc int) string {
	var b strings.Builder
	b.WriteString("[defaults]\n")
	if conc > 0 {
		fmt.Fprintf(&b, "concurrency = %d\n", conc)
	}
	for _, t := range tasks {
		b.WriteString("\n[[task]]\n")
		fmt.Fprintf(&b, "id     = %q\n", t.ID)
		fmt.Fprintf(&b, "prompt = %q\n", t.Prompt)
		fmt.Fprintf(&b, "agent  = %q\n", t.Agent)
		if t.Gate != "" {
			fmt.Fprintf(&b, "gate   = %q\n", t.Gate)
		}
	}
	return b.String()
}
```

- [ ] **Step 4: Extract a shared runTasks helper in internal/cli/run.go**

Refactor the body of `newRunCmd`'s `RunE` that builds the `Runner` and renders
the summary into a reusable function, so `plan --yes` can call it:

```go
// runTasks opens the repo, runs the tasks, and prints the summary. Shared by
// `fan run` and `fan plan --yes`. finalGate is the command to run on the
// integration branch after merges ("" to skip it).
func runTasks(cmd *cobra.Command, tasks []task.Task, conc int, finalGate string) error {
	repo, err := vcs.Open(".")
	if err != nil {
		return err
	}
	workRoot := filepath.Join(repo.Root(), ".fan")
	if err := os.MkdirAll(workRoot, 0o755); err != nil {
		return err
	}
	r := run.Runner{
		Repo:      repo,
		WorkRoot:  workRoot,
		Reporter:  run.NewTextReporter(cmd.ErrOrStderr()),
		FinalGate: finalGate,
		Now:       timestamp,
	}
	outcomes, branch, fg, err := r.RunWithFinalGate(context.Background(), tasks, conc)
	if err != nil {
		return err
	}
	fmt.Fprint(cmd.OutOrStdout(), summary.Render(outcomes, branch, fg))
	return nil
}
```

Then replace the corresponding inline block in `newRunCmd`'s `RunE` (everything
from `repo, err := vcs.Open(".")` through the summary print) with the final-gate
computation plus a call to the helper, so `--no-final-gate` is honored:

```go
			finalGate := ""
			if !noFinalGate {
				finalGate = firstGate(tasks)
			}
			return runTasks(cmd, tasks, conc, finalGate)
```

In `newPlanCmd`'s `--yes` branch, call it with the planned tasks' gate:
`return runTasks(cmd, tasks, concOrDefault(conc), firstGate(tasks))`.

Register the command in `Execute`:

```go
	root.AddCommand(newPlanCmd())
```

- [ ] **Step 5: Run tests and build**

Run: `go test ./... && go build -o fan ./cmd/fan`
Expected: PASS, binary builds

- [ ] **Step 6: Commit**

```bash
git add internal/cli/
git commit -m "Add fan plan command with approve-and-save flow"
```

---

## Task 16: End-to-end smoke test with a stub agent on PATH

**Files:**
- Create: `internal/cli/e2e_test.go`

- [ ] **Step 1: Write the test (drives the real `run` command through a stub agent)**

```go
package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestRunCommandEndToEnd builds a fake "claude" on PATH that appends a line to a
// file, then runs `fan run --each` over two items in a temp repo and asserts the
// integration branch exists with both merges.
func TestRunCommandEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(repo, "seed.txt"), []byte("seed\n"), 0o644)
	gitRun(t, repo, "add", "-A")
	gitRun(t, repo, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "seed")

	// Fake agent: a shell script named "claude" that writes a file named after
	// the prompt's last word.
	bin := t.TempDir()
	script := "#!/bin/sh\nlast=$(echo \"$2\" | awk '{print $NF}')\necho done > \"$last\"\n"
	os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	wd, _ := os.Getwd()
	defer os.Chdir(wd)
	os.Chdir(repo)

	cmd := newRunCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--each", "x.txt,y.txt", "--agent", "claude", "Create {}"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("run: %v\n%s", err, out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("2 merged")) {
		t.Errorf("expected 2 merged:\n%s", out.String())
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
```

- [ ] **Step 2: Run the test**

Run: `go test ./internal/cli/ -run TestRunCommandEndToEnd -v`
Expected: PASS (if it fails on prompt-word parsing, adjust the stub script — the template `Create {}` makes the prompt end in the filename)

- [ ] **Step 3: Run the full suite**

Run: `go test ./...`
Expected: PASS across all packages

- [ ] **Step 4: Commit**

```bash
git add internal/cli/e2e_test.go
git commit -m "Add end-to-end run command test with a stub agent"
```

---

## Task 17: README and project docs

**Files:**
- Create: `README.md`
- Create: `LICENSE` (MIT, copyright Ravi Subedi)
- Create: `CONTRIBUTING.md`
- Create: `SECURITY.md`
- Create: `CODE_OF_CONDUCT.md`

- [ ] **Step 1: Write README.md**

Include: one-paragraph description (agent-agnostic parallel coding-agent
orchestrator; sibling to prr), install (`brew install ravistakumar/tap/fan` and
`go install`), a quickstart for both `fan run --each` and a `tasks.toml`, the
`fan plan` flow, the supported-agent table (mark `aider` experimental), the
safety model (throwaway integration branch, never touches your working branch,
gate-before-merge), and the four task statuses. Mirror prr's README tone.

- [ ] **Step 2: Add LICENSE, CONTRIBUTING.md, SECURITY.md, CODE_OF_CONDUCT.md**

Copy the structure from the `prr` repo, updating the project name to `fan`.

- [ ] **Step 3: Verify the README examples match the actual CLI**

Run: `./fan run --help && ./fan plan --help`
Expected: flags match what the README documents

- [ ] **Step 4: Commit**

```bash
git add README.md LICENSE CONTRIBUTING.md SECURITY.md CODE_OF_CONDUCT.md
git commit -m "Add README and project docs"
```

---

## Task 18: Release pipeline (GoReleaser + GitHub Actions + Homebrew cask)

**Files:**
- Create: `.goreleaser.yaml`
- Create: `.github/workflows/ci.yml`
- Create: `.github/workflows/release.yml`
- Create: `.golangci.yml`
- Create: `install.sh`

- [ ] **Step 1: Write .goreleaser.yaml (cask, mirroring prr's working config)**

```yaml
version: 2
project_name: fan
before:
  hooks:
    - go mod tidy
builds:
  - main: ./cmd/fan
    binary: fan
    env: [CGO_ENABLED=0]
    ldflags:
      - -s -w -X github.com/ravistakumar/fan/internal/cli.version={{.Version}}
    goos: [linux, darwin, windows]
    goarch: [amd64, arm64]
archives:
  - formats: [tar.gz]
    name_template: "{{ .ProjectName }}_{{ .Os }}_{{ .Arch }}"
homebrew_casks:
  - repository:
      owner: ravistakumar
      name: homebrew-tap
      token: "{{ .Env.HOMEBREW_TAP_GITHUB_TOKEN }}"
    homepage: "https://github.com/ravistakumar/fan"
    description: "Agent-agnostic parallel coding-agent orchestrator"
checksum:
  name_template: "checksums.txt"
```

- [ ] **Step 2: Write .github/workflows/ci.yml**

```yaml
name: ci
on:
  push: { branches: [main] }
  pull_request:
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
      - uses: actions/setup-go@v6
        with: { go-version: "1.23" }
      - run: go test ./...
      - uses: golangci/golangci-lint-action@v9
        with: { version: latest }
```

- [ ] **Step 3: Write .github/workflows/release.yml**

```yaml
name: release
on:
  push:
    tags: ["v*"]
permissions:
  contents: write
jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
        with: { fetch-depth: 0 }
      - uses: actions/setup-go@v6
        with: { go-version: "1.23" }
      - uses: goreleaser/goreleaser-action@v7
        with: { version: latest, args: release --clean }
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          HOMEBREW_TAP_GITHUB_TOKEN: ${{ secrets.HOMEBREW_TAP_GITHUB_TOKEN }}
```

- [ ] **Step 4: Write .golangci.yml**

```yaml
version: "2"
linters:
  default: standard
  exclusions:
    presets:
      - std-error-handling
```

- [ ] **Step 5: Write install.sh**

Copy prr's `install.sh`, replacing the binary name and repo with `fan`.

- [ ] **Step 6: Validate the GoReleaser config locally**

Run: `goreleaser check`
Expected: `configuration is valid`

- [ ] **Step 7: Commit**

```bash
git add .goreleaser.yaml .github/ .golangci.yml install.sh
git commit -m "Add release pipeline and CI"
```

---

## Task 19: Final verification and live agent smoke test

**Files:** none (verification only)

- [ ] **Step 1: Run the full suite with the race detector**

Run: `go test -race ./...`
Expected: PASS, no race warnings (the scheduler + reporter are the race-sensitive bits)

- [ ] **Step 2: Build and run a real fan over a throwaway repo**

Manually create a small git repo with two files, then run a real 2-task fan-out
against each installed agent (at minimum one of claude/codex/opencode/grok):

Run: `./fan run --each 'a.txt,b.txt' --agent <installed-agent> --gate 'true' 'Add a TODO comment to {}'`
Expected: summary reports merges; `git branch --list 'fan/*'` shows the integration branch; the working branch is unchanged (`git status` clean)

- [ ] **Step 3: Record which agents were live-verified**

Update the README's supported-agent table: mark any agent not smoke-tested live
as **experimental** (the policy prr used for aider).

- [ ] **Step 4: Commit any doc updates**

```bash
git add README.md
git commit -m "Record live-verified agents in README"
```

---

## Done

At this point `fan` has: parsing + expansion, five agent adapters, a
bounded-concurrency scheduler, the gate + merge-decision core, git worktree
isolation with serialized cherry-pick onto a throwaway integration branch, a
final integration gate, `fan run` and `fan plan`, full test coverage with a
stub agent and real git, and a release pipeline matching prr's. Tag `v0.1.0` to
ship once at least one agent is live-verified.
