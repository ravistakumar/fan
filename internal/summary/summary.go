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
