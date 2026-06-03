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
