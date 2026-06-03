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
