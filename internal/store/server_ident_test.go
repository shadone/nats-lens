package store

import (
	"slices"
	"testing"

	"github.com/shadone/nats-lens/internal/collect"
)

type serverRow struct {
	rowid     int64
	tags      []string
	lastEpoch int64
}

func readServer(t *testing.T, s *Store, id string) serverRow {
	t.Helper()
	var r serverRow
	var tags []any
	if err := s.DB().QueryRow(`SELECT rowid, tags, last_epoch FROM servers WHERE server_id = ?`, id).
		Scan(&r.rowid, &tags, &r.lastEpoch); err != nil {
		t.Fatal(err)
	}
	for _, x := range tags {
		r.tags = append(r.tags, x.(string))
	}
	return r
}

// Writing a known server again updates its row in place: the row keeps its
// rowid, so the primary key index is left as it was.
func TestServerIdentUpdatesInPlace(t *testing.T) {
	s := openMem(t)
	writeEpochs(t, s, 3, func(int, *collect.Snapshot) {})
	got := readServer(t, s, "NFAKESERVER0ID")
	if got.lastEpoch != 3 {
		t.Fatalf("last_epoch = %d, want 3", got.lastEpoch)
	}
	first := readServer(t, s, "NFAKESERVER1ID")
	writeEpochs(t, s, 1, func(int, *collect.Snapshot) {})
	if again := readServer(t, s, "NFAKESERVER1ID"); again.rowid != first.rowid {
		t.Fatalf("rowid moved from %d to %d: the upsert deleted and reinserted the row", first.rowid, again.rowid)
	}
	var rows int
	if err := s.DB().QueryRow(`SELECT count(*) FROM servers`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 2 {
		t.Fatalf("servers rows = %d, want 2", rows)
	}
}

// A server's tags follow its latest report, and an unchanged list leaves
// the row where it is.
func TestServerTagsFollowTheReport(t *testing.T) {
	s := openMem(t)
	tags := [][]string{{"zone:a"}, {"zone:b", "ssd"}, {"zone:b", "ssd"}, nil}
	var rowids []int64
	for i, want := range tags {
		writeEpochs(t, s, 1, func(_ int, snap *collect.Snapshot) {
			v := snap.Servers["NFAKESERVER0ID"]
			v.Info.Tags = want
		})
		got := readServer(t, s, "NFAKESERVER0ID")
		if !slices.Equal(got.tags, want) {
			t.Fatalf("epoch %d: tags = %v, want %v", i+1, got.tags, want)
		}
		rowids = append(rowids, got.rowid)
	}
	if rowids[2] != rowids[1] {
		t.Fatalf("unchanged tags moved the row from %d to %d", rowids[1], rowids[2])
	}
}
