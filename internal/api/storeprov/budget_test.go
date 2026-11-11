package storeprov_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/admit"
	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
	"github.com/shadone/nats-lens/internal/timing"
)

// waitUntil polls cond until it holds or 20 seconds pass.
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func budgetedStore(t *testing.T, id string, n int) *store.Store {
	t.Helper()
	st := emptyStore(t)
	st.SetSystem(id)
	st.SetQueryBudget(n)
	for i := range 3 {
		if _, err := st.WriteSnapshot(ctx, snapshot(t, i)); err != nil {
			t.Fatal(err)
		}
	}
	return st
}

// One system whose budget is held by a query that never ends answers its
// callers resource_exhausted once their deadline passes in the queue; a
// second system in the same process answers at once; and canceling the
// held query returns both budgets to empty.
func TestOverloadedSystemBesideAHealthyOne(t *testing.T) {
	busy := budgetedStore(t, "budget-busy", 1)
	calm := budgetedStore(t, "budget-calm", 1)
	clock := func() time.Time { return t0.Add(2 * time.Minute) }
	prov := func(st *store.Store) api.Provider {
		return &storeprov.Provider{St: st, Interval: 30 * time.Second, Live: true, Clock: clock}
	}
	srv, c := apitest.ServeHandler(t, &api.Handler{Systems: api.Systems{
		{Ref: &v1.SystemRef{Id: "budget-busy"}, Provider: prov(busy)},
		{Ref: &v1.SystemRef{Id: "budget-calm"}, Provider: prov(calm)},
	}, MetricsTimeout: time.Second})

	hctx, hcancel := context.WithCancel(admit.WithFlow(timing.WithSource(context.Background(), timing.SourceAPI)))
	held := make(chan error, 1)
	go func() {
		var v any
		held <- busy.DB().QueryRowContext(hctx, `SELECT sum(i) FROM range(1000000000000) t(i)`).Scan(&v)
	}()
	waitUntil(t, "the busy system's slot taken", func() bool { a, _ := busy.QueryBudget().Load(); return a == 1 })

	if _, err := c.ListServers(ctx, connect.NewRequest(&v1.ListServersRequest{System: "budget-calm"})); err != nil {
		t.Fatalf("the healthy system: %v", err)
	}

	// The client allows 2 s; the server's call ends with that deadline
	// while its first query still waits, and answers resource_exhausted.
	rctx, rcancel := context.WithTimeout(ctx, 2*time.Second)
	defer rcancel()
	before := timing.APIDuration.Count("budget-busy", "ListServers", connect.CodeResourceExhausted.String())
	_, err := c.ListServers(rctx, connect.NewRequest(&v1.ListServersRequest{System: "budget-busy"}))
	if err == nil {
		t.Fatal("the busy system answered")
	}
	waitUntil(t, "the busy call recorded as resource_exhausted", func() bool {
		return timing.APIDuration.Count("budget-busy", "ListServers", connect.CodeResourceExhausted.String()) == before+1
	})
	waitUntil(t, "no busy call in flight", func() bool {
		return timing.APIInFlight.With("budget-busy", "ListServers").Load() == 0
	})

	body := scrape(t, srv.URL+"/metrics")
	for _, typ := range []string{
		"nats_lens_db_admission_wait_seconds histogram",
		"nats_lens_db_admission_active gauge",
		"nats_lens_db_admission_queued gauge",
		"nats_lens_db_admission_limit gauge",
	} {
		if !strings.Contains(body, "# TYPE "+typ+"\n") {
			t.Errorf("missing # TYPE %s", typ)
		}
	}
	if v := sampleValue(t, body, `nats_lens_db_admission_limit{system="budget-busy"}`); v != 1 {
		t.Errorf("limit %g", v)
	}
	if v := sampleValue(t, body, `nats_lens_db_admission_wait_seconds_count{system="budget-busy",source="api",outcome="deadline"}`); v < 1 {
		t.Errorf("deadline outcomes %g", v)
	}
	// The held query keeps the busy system's slot, so its part of the
	// scrape runs out of time in the queue; the calm system's is read.
	if v := sampleValue(t, body, `nats_lens_system_scrape_success{system="budget-calm"}`); v != 1 {
		t.Errorf("calm scrape success %g", v)
	}
	if v := sampleValue(t, body, `nats_lens_system_scrape_success{system="budget-busy"}`); v != 0 {
		t.Errorf("busy scrape success %g", v)
	}
	if v := sampleValue(t, body, `nats_lens_db_admission_wait_seconds_count{system="budget-busy",source="metrics",outcome="deadline"}`); v < 1 {
		t.Errorf("metrics deadline outcomes %g", v)
	}

	hcancel()
	if err := <-held; !errors.Is(err, context.Canceled) {
		t.Fatalf("held query: %v", err)
	}
	for _, st := range []*store.Store{busy, calm} {
		waitUntil(t, "budgets empty", func() bool { a, q := st.QueryBudget().Load(); return a == 0 && q == 0 })
	}
	if _, err := c.ListServers(ctx, connect.NewRequest(&v1.ListServersRequest{System: "budget-busy"})); err != nil {
		t.Fatalf("the busy system after the cancel: %v", err)
	}
}
