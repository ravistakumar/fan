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
