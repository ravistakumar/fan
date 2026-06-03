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
