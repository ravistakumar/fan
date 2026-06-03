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
