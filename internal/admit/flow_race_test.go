package admit

import (
	"context"
	"sync"
	"testing"
)

// Two goroutines that share one flow and enter the gate at the same time
// must not leave a slot taken once both have left.
func TestSharedFlowConcurrentEnterLeaksNoSlot(t *testing.T) {
	for i := range 2000 {
		g := New(4, nil)
		ctx := WithFlow(context.Background())
		var wg sync.WaitGroup
		start := make(chan struct{})
		rel := make([]func(), 2)
		for j := range 2 {
			wg.Go(func() {
				<-start
				r, err := g.Enter(ctx, Request{Source: "api"})
				if err != nil {
					t.Error(err)
					return
				}
				rel[j] = r
			})
		}
		close(start)
		wg.Wait()
		for _, r := range rel {
			r()
		}
		if a, _ := g.Load(); a != 0 {
			t.Fatalf("iteration %d: %d slot(s) still taken after both queries of one flow left", i, a)
		}
	}
}
