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
