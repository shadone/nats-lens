package collect

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"

	"github.com/shadone/nats-lens/internal/testcluster"
)

// subszCollector returns a collector on nc whose clock reads *at.
func subszCollector(nc *nats.Conn, interval time.Duration, limit int, at *time.Time) *Collector {
	c := New(nc, Options{
		Window: 500 * time.Millisecond, ConnzLimit: 100, Tracker: NewTracker(time.Hour),
		SubscriptionsInterval: interval, SubszLimit: limit,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	c.now = func() time.Time { return *at }
	return c
}

// asksSubsz reports whether the snapshot's server answered SUBSZ, and
// fails the test when it has a SUBSZ status other than ok.
func asksSubsz(t *testing.T, snap *Snapshot, id string) bool {
	t.Helper()
	v := snap.Servers[id]
	if v == nil {
		t.Fatalf("server %s did not answer", id)
	}
	st, ok := v.Status[Subsz]
	if !ok {
		if v.Subsz != nil {
			t.Fatal("SUBSZ reply without a status")
		}
		return false
	}
	if st.State != StateOK || v.Subsz == nil {
		t.Fatalf("SUBSZ status %+v, reply %v", st, v.Subsz != nil)
	}
	return true
}

// SUBSZ is asked in the first epoch, not again within the interval, again
// once the interval has passed since the last collection started; an
// interval of 0 never asks.
func TestSubszInterval(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a server")
	}
	ctx := context.Background()
	c := testcluster.Start(t, 1)
	app := c.Connect(0, testcluster.AppUser, testcluster.AppPass)
	if _, err := app.Subscribe("orders.created", func(*nats.Msg) {}); err != nil {
		t.Fatal(err)
	}
	if err := app.Flush(); err != nil {
		t.Fatal(err)
	}
	id := c.Servers[0].ID()
	at := time.Date(2026, 10, 22, 14, 0, 0, 0, time.UTC)
	col := subszCollector(c.Connect(0, testcluster.SysUser, testcluster.SysPass), 5*time.Minute, 100, &at)

	steps := []struct {
		after time.Duration
		asked bool
	}{
		{0, true},
		{30 * time.Second, false},
		{4 * time.Minute, false},
		// 5 minutes after the first, less a few milliseconds of jitter.
		{29*time.Second + 990*time.Millisecond, true},
		{4*time.Minute + 50*time.Second, false},
		{10 * time.Second, true},
	}
	for i, s := range steps {
		at = at.Add(s.after)
		snap := col.Collect(ctx)
		if got := asksSubsz(t, snap, id); got != s.asked {
			t.Fatalf("epoch %d at %s: asked SUBSZ %v, want %v", i, at.Format(time.TimeOnly), got, s.asked)
		}
		if s.asked {
			found := false
			for _, d := range snap.Servers[id].Subsz.Subs {
				if d.Subject == "orders.created" && d.Account == "APP" {
					found = true
				}
			}
			if !found {
				t.Fatalf("epoch %d: the client's subscription is not listed: %+v", i, snap.Servers[id].Subsz.Subs)
			}
		}
		if snap.Partial() {
			t.Fatalf("epoch %d partial: %v", i, snap.Errors)
		}
	}

	off := subszCollector(c.Connect(0, testcluster.SysUser, testcluster.SysPass), 0, 100, &at)
	for range 3 {
		at = at.Add(time.Hour)
		if asksSubsz(t, off.Collect(ctx), id) {
			t.Fatal("SUBSZ asked with subscriptions_interval 0")
		}
	}
}

// A server lists at most subsz_limit subscriptions and says how many it
// holds.
func TestSubszLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a server")
	}
	ctx := context.Background()
	c := testcluster.Start(t, 1)
	app := c.Connect(0, testcluster.AppUser, testcluster.AppPass)
	for _, subj := range []string{"orders.a", "orders.b", "orders.c", "orders.d", "orders.e"} {
		if _, err := app.Subscribe(subj, func(*nats.Msg) {}); err != nil {
			t.Fatal(err)
		}
	}
	if err := app.Flush(); err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	col := subszCollector(c.Connect(0, testcluster.SysUser, testcluster.SysPass), time.Minute, 3, &at)
	sz := col.Collect(ctx).Servers[c.Servers[0].ID()].Subsz
	if sz == nil {
		t.Fatal("no SUBSZ reply")
	}
	if len(sz.Subs) != 3 || sz.Limit != 3 {
		t.Errorf("listed %d with limit %d, want 3", len(sz.Subs), sz.Limit)
	}
	if sz.Total < 5 {
		t.Errorf("total %d, want at least the client's 5", sz.Total)
	}
}

// subszSilent is a fake server that answers STATSZ only.
func subszSilent(t *testing.T, nc *nats.Conn) {
	t.Helper()
	info := server.ServerInfo{Name: "silent", ID: "IDSILENT"}
	statsz, _ := json.Marshal(server.ServerStatsMsg{Server: info})
	if _, err := nc.Subscribe("$SYS.REQ.SERVER.PING.STATSZ", func(m *nats.Msg) { _ = m.Respond(statsz) }); err != nil {
		t.Fatal(err)
	}
	if err := nc.Flush(); err != nil {
		t.Fatal(err)
	}
}

// SUBSZ is recorded missing for a server only in an epoch that asked it.
func TestSubszMissingOnlyWhenAsked(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a server")
	}
	ctx := context.Background()
	c := testcluster.Start(t, 1)
	nc := c.Connect(0, testcluster.SysUser, testcluster.SysPass)
	subszSilent(t, nc)
	at := time.Now()
	col := subszCollector(nc, 5*time.Minute, 100, &at)

	snap := col.Collect(ctx)
	if st := snap.Servers["IDSILENT"].Status[Subsz]; st.State != StateMissing {
		t.Errorf("asked epoch: silent server's SUBSZ = %+v, want missing", st)
	}
	at = at.Add(time.Minute)
	snap = col.Collect(ctx)
	if st, ok := snap.Servers["IDSILENT"].Status[Subsz]; ok {
		t.Errorf("epoch that did not ask SUBSZ records %+v for it", st)
	}
	if _, ok := snap.Servers[c.Servers[0].ID()].Status[Subsz]; ok {
		t.Error("epoch that did not ask SUBSZ has a status for the real server")
	}
}
