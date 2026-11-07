package store

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/collect"
)

// identKeys are identifiers that need quoting or escaping somewhere.
var identKeys = []string{"plain", "q'uote", `dq"uote`, `back\slash`, "tab\tnew\nline", "nul\x00byte",
	"emoji \U0001F600", "ls ps ", "null", "%_like", ""}

// fillIdents records epoch e's identities. Over the epochs it covers:
// a new row, a conflict with a stored value and a NULL in the new row, a
// conflict with a new non-NULL value, a name equal to the identifier
// (keep stored), is_system set once, and batches over jsonBatch that mix
// conflicting and new keys across the boundary.
func fillIdents(w *writer, e int64) {
	n := jsonBatch + 7
	base := time.Date(2026, 10, 1, 0, 30, 0, 123456789, time.UTC)
	for _, k := range identKeys {
		if k == "" {
			continue
		}
		switch e {
		case 1:
			w.ids.account("A"+k, "name-"+k, false)
		case 2:
			w.ids.account("A"+k, "", k == "plain")
		case 3:
			w.ids.account("A"+k, "A"+k, false)
			w.ids.account("B"+k, "B"+k, false)
		}
	}
	for _, k := range identKeys {
		var c time.Time
		if e != 2 {
			c = base.Add(time.Duration(e) * time.Hour)
		}
		w.ids.stream(k, "s-"+k, c)
		w.ids.consumer(k, "s-"+k, "c-"+k, c)
		if e == 1 {
			w.ids.raftAsset("S-"+k, k, "s-"+k, "")
			w.ids.raftAsset("C-"+k, k, "s-"+k, "c-"+k)
		} else {
			w.ids.raftGroup("S-" + k)
			w.ids.raftGroup("C-" + k)
		}
	}
	// Epoch 1 writes the first n streams; epoch 2 conflicts with them,
	// alternately without and with a creation time, and adds n new ones;
	// epoch 3 conflicts with all 2n.
	lo, hi := 0, n
	if e >= 2 {
		hi = 2 * n
	}
	for i := lo; i < hi; i++ {
		var c time.Time
		if e != 2 || i%2 == 1 {
			c = base.Add(time.Duration(e)*time.Minute + time.Duration(i)*time.Microsecond)
		}
		w.ids.stream("BULK", fmt.Sprintf("b-%05d", i), c)
		w.ids.consumer("BULK", fmt.Sprintf("b-%05d", i), "c", c)
		w.ids.raftGroup(fmt.Sprintf("S-b-%05d", i))
		if e == 1 && i%3 == 0 {
			w.ids.raftAsset(fmt.Sprintf("S-b-%05d", i), "BULK", fmt.Sprintf("b-%05d", i), "")
		}
	}
}

// writeIdentEpochs writes three epochs; byRow says, per epoch, which path.
func writeIdentEpochs(t *testing.T, path string, byRow [3]bool) {
	t.Helper()
	ctx := context.Background()
	s, err := OpenPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	prev := int64(0)
	for i, e := range []int64{1, 2, 3} {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		w := newWriter(ctx, tx, e, collect.NewSnapshot(t0))
		w.prevEpoch = prev
		w.log = slog.New(slog.DiscardHandler)
		fillIdents(w, e)
		if byRow[i] {
			w.writeIdentsByRow()
		} else {
			w.writeIdents()
		}
		w.closeStatements()
		if w.err != nil {
			_ = tx.Rollback()
			t.Fatalf("epoch %d: %v", e, w.err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		prev = e
	}
}

// TestIdentsMatchByRowAtEdges compares the batch path, and an upgrade
// (epoch 1 by row, then batches), with the row-at-a-time reference, for
// identifiers that need quoting and batches of conflicting and new keys
// across the batch size. The reference takes about two minutes, so -short
// skips it.
func TestIdentsMatchByRowAtEdges(t *testing.T) {
	if testing.Short() {
		t.Skip("long")
	}
	dir := t.TempDir()
	ref := filepath.Join(dir, "ref.duckdb")
	writeIdentEpochs(t, ref, [3]bool{true, true, true})
	for name, mode := range map[string][3]bool{"batch": {}, "upgrade": {true, false, false}} {
		t.Run(name, func(t *testing.T) {
			got := filepath.Join(dir, name+".duckdb")
			writeIdentEpochs(t, got, mode)
			s, err := OpenPath(context.Background(), got)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			db := s.DB()
			if _, err := db.Exec(`ATTACH ` + sqlString(ref) + ` AS ref (READ_ONLY)`); err != nil {
				t.Fatal(err)
			}
			for _, table := range []string{"accounts", "streams", "consumers", "raft_groups"} {
				for _, q := range []string{
					`SELECT * FROM ref.` + table + ` EXCEPT ALL SELECT * FROM main.` + table,
					`SELECT * FROM main.` + table + ` EXCEPT ALL SELECT * FROM ref.` + table,
				} {
					if d := count(t, db, `SELECT count(*) FROM (`+q+`)`); d != 0 {
						t.Errorf("%s: %d rows differ: %s", table, d, q)
					}
				}
			}
			// Spot checks against the scenario, not the reference.
			n := jsonBatch + 7
			for _, c := range []struct {
				q    string
				want int
			}{
				{`SELECT count(*) FROM streams WHERE account = 'BULK'`, 2 * n},
				{`SELECT count(*) FROM streams WHERE account = 'BULK' AND first_epoch = 1 AND last_epoch = 3`, n},
				{`SELECT count(*) FROM streams WHERE account = 'BULK' AND created = TIMESTAMP '2026-10-01 00:33:00.123456' + to_microseconds(CAST(substr(stream, 3) AS BIGINT))`, 2 * n},
				{`SELECT count(*) FROM raft_groups WHERE group_id LIKE 'S-b-%' AND stream IS NOT NULL`, (n + 2) / 3},
				{`SELECT count(*) FROM accounts WHERE account = 'Aplain' AND account_name = 'name-plain' AND is_system AND last_epoch = 3`, 1},
				{`SELECT count(*) FROM accounts WHERE account = 'Anul' || chr(0) || 'byte' AND account_name = 'name-nul' || chr(0) || 'byte'`, 1},
				{`SELECT count(*) FROM streams WHERE account = '' AND stream = 's-' AND created = TIMESTAMP '2026-10-01 03:30:00.123456'`, 1},
				{`SELECT count(*) FROM raft_groups WHERE group_id = 'C-q''uote' AND account = 'q''uote' AND consumer = 'c-q''uote' AND last_epoch = 3`, 1},
				{`SELECT count(*) FROM accounts`, 2*(len(identKeys)-1) + 0},
			} {
				if got := count(t, db, c.q); got != c.want {
					t.Errorf("%s = %d, want %d", c.q, got, c.want)
				}
			}
		})
	}
}
