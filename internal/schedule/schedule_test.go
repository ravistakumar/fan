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
