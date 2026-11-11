package store

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/collect"
)

type serverRow struct {
	rowid     int64
	tags      []string
	tagsNull  bool
	lastEpoch int64
}

func readServer(t *testing.T, s *Store, id string) serverRow {
	t.Helper()
	var r serverRow
	var tags []any
	if err := s.DB().QueryRow(`SELECT rowid, tags, tags IS NULL, last_epoch FROM servers WHERE server_id = ?`, id).
		Scan(&r.rowid, &tags, &r.tagsNull, &r.lastEpoch); err != nil {
		t.Fatal(err)
	}
	for _, x := range tags {
		r.tags = append(r.tags, x.(string))
	}
	return r
}

// openMemNoCheckpoint is openMem without automatic checkpoints. A checkpoint
// vacuums a table's deleted rows and renumbers the rest, so a rowid moves
// without any write to the row; with it off, only a write that deletes and
// reinserts the row moves its rowid.
func openMemNoCheckpoint(t *testing.T) *Store {
	t.Helper()
	s := openMem(t)
	if _, err := s.DB().Exec(`SET checkpoint_threshold = '10GB'`); err != nil {
		t.Fatal(err)
	}
	return s
}

// writeEpochAt writes the fixture snapshot of epoch n, started n-1
// intervals after t0, after edit changed it.
func writeEpochAt(t *testing.T, s *Store, n int, edit func(snap *collect.Snapshot)) {
	t.Helper()
	snap := fixtureSnapshot(t, t0.Add(time.Duration(n-1)*30*time.Second))
	edit(snap)
	if _, err := s.WriteSnapshot(context.Background(), snap); err != nil {
		t.Fatal(err)
	}
}

// Writing a known server again updates its row in place: the row keeps its
// rowid, so the primary key index is left as it was.
func TestServerIdentUpdatesInPlace(t *testing.T) {
	s := openMemNoCheckpoint(t)
	for n := 1; n <= 3; n++ {
		writeEpochAt(t, s, n, func(*collect.Snapshot) {})
	}
	if got := readServer(t, s, "NFAKESERVER0ID"); got.lastEpoch != 3 {
		t.Fatalf("last_epoch = %d, want 3", got.lastEpoch)
	}
	first := readServer(t, s, "NFAKESERVER1ID")
	writeEpochAt(t, s, 4, func(*collect.Snapshot) {})
	if again := readServer(t, s, "NFAKESERVER1ID"); again.rowid != first.rowid {
		t.Fatalf("rowid moved from %d to %d: the write deleted and reinserted the row", first.rowid, again.rowid)
	}
	var rows int
	if err := s.DB().QueryRow(`SELECT count(*) FROM servers`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 2 {
		t.Fatalf("servers rows = %d, want 2", rows)
	}
}

// A server's tags follow its latest report, in its order, and emptied tags
// are an empty list; an unchanged list leaves the row where it is.
func TestServerTagsFollowTheReport(t *testing.T) {
	s := openMemNoCheckpoint(t)
	tags := [][]string{{"zone:a"}, {"zone:b", "ssd"}, {"zone:b", "ssd"}, {"ssd", "zone:b"}, nil}
	var rowids []int64
	for i, want := range tags {
		writeEpochAt(t, s, i+1, func(snap *collect.Snapshot) {
			snap.Servers["NFAKESERVER0ID"].Info.Tags = want
		})
		got := readServer(t, s, "NFAKESERVER0ID")
		if got.tagsNull {
			t.Fatalf("epoch %d: tags are NULL, want %v", i+1, want)
		}
		if !slices.Equal(got.tags, want) {
			t.Fatalf("epoch %d: tags = %v, want %v", i+1, got.tags, want)
		}
		rowids = append(rowids, got.rowid)
	}
	if rowids[2] != rowids[1] {
		t.Fatalf("unchanged tags moved the row from %d to %d: the write deleted and reinserted the row", rowids[1], rowids[2])
	}
}
