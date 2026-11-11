package store

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/shadone/nats-lens/internal/timing"
)

// A query hook receives each query's text, arguments and procedure; with
// none set, the query is only timed.
func TestQueryHookSeesQueries(t *testing.T) {
	s := openMem(t)
	s.SetSystem("hook-test")
	var mu sync.Mutex
	var got []timing.QueryInfo
	timing.SetQueryHook(func(q timing.QueryInfo) {
		mu.Lock()
		got = append(got, q)
		mu.Unlock()
	})
	defer timing.SetQueryHook(nil)
	ctx := timing.WithCall(timing.WithSource(context.Background(), timing.SourceAPI), &timing.Call{Procedure: "GetOverview"})
	var n int
	if err := s.DB().QueryRowContext(ctx, `SELECT count(*) FROM epochs WHERE epoch > ?`, 7).Scan(&n); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, q := range got {
		if strings.Contains(q.SQL, "FROM epochs WHERE epoch > ?") {
			if q.Procedure != "GetOverview" || q.Source != timing.SourceAPI || q.System != "hook-test" || len(q.Args) != 1 || q.Args[0] != int64(7) {
				t.Fatalf("hook got %+v", q)
			}
			return
		}
	}
	t.Fatalf("the hook did not see the query; it saw %d", len(got))
}
