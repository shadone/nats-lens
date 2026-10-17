package store

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"

	"github.com/shadone/nats-lens/internal/collect"
)

type run struct{ first, last int64 }

func configRuns(t *testing.T, s *Store, serverID, stream string) []run {
	t.Helper()
	rows, err := s.DB().Query(`SELECT first_epoch, last_epoch FROM stream_config
WHERE server_id = ? AND stream = ? ORDER BY first_epoch`, serverID, stream)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []run
	for rows.Next() {
		var r run
		if err := rows.Scan(&r.first, &r.last); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func writeEpochs(t *testing.T, s *Store, n int, edit func(i int, snap *collect.Snapshot)) {
	t.Helper()
	for i := range n {
		snap := fixtureSnapshot(t, t0.Add(time.Duration(i)*30*time.Second))
		edit(i, snap)
		if _, err := s.WriteSnapshot(context.Background(), snap); err != nil {
			t.Fatal(err)
		}
	}
}

// A server that reports nothing in epoch 2 must not have a run covering it.
func TestConfigRunBreaksWhenServerMissing(t *testing.T) {
	s := openMem(t)
	writeEpochs(t, s, 3, func(i int, snap *collect.Snapshot) {
		if i == 1 {
			delete(snap.Servers, "NFAKESERVER1ID")
		}
	})
	got := configRuns(t, s, "NFAKESERVER1ID", "catalog-changes")
	want := []run{{1, 1}, {3, 3}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("runs = %v, want %v", got, want)
	}
	// The other server's run is unbroken.
	if got := configRuns(t, s, "NFAKESERVER0ID", "catalog-changes"); len(got) != 1 || got[0] != (run{1, 3}) {
		t.Fatalf("nats-0 runs = %v", got)
	}
}

// A server that answers but whose JSZ is missing reports no stream either.
func TestConfigRunBreaksWhenJSZMissing(t *testing.T) {
	s := openMem(t)
	writeEpochs(t, s, 3, func(i int, snap *collect.Snapshot) {
		if i == 1 {
			snap.Servers["NFAKESERVER1ID"].Jsz = nil
		}
	})
	if got := configRuns(t, s, "NFAKESERVER1ID", "catalog-changes"); len(got) != 2 {
		t.Fatalf("runs = %v, want two", got)
	}
}

// A to B and back to A gives three runs, not one run for A spanning B.
func TestConfigRunFlipFlop(t *testing.T) {
	s := openMem(t)
	writeEpochs(t, s, 4, func(i int, snap *collect.Snapshot) {
		if i == 1 {
			changeStream(snap, "NFAKESERVER1ID", "catalog-changes", func(c *server.StreamConfig) { c.MaxBytes = 1 << 30 })
		}
	})
	got := configRuns(t, s, "NFAKESERVER1ID", "catalog-changes")
	want := []run{{1, 1}, {2, 2}, {3, 4}}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("runs = %v, want %v", got, want)
	}
}
