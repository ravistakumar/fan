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
