package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// referenceStreamSamples computes stream_samples from every stored report,
// as the per-stream series did before the table: one row per stream and
// epoch, the leader's value else the largest, a creation time only when
// all reports agree, and the previous row by lag over the stream's rows in
// epoch order.
var referenceStreamSamples = `
WITH s AS (
  SELECT x.epoch, x.account, x.stream,
    coalesce(max(x.bytes) FILTER (WHERE x.is_leader), max(x.bytes)) AS bytes,
    coalesce(max(x.messages) FILTER (WHERE x.is_leader), max(x.messages)) AS messages,
    sum(x.bytes) AS replica_bytes,
    coalesce(max(x.last_seq) FILTER (WHERE x.is_leader), max(x.last_seq)) AS seq,
    CASE WHEN count(x.created) = count(*) AND count(DISTINCT x.created) = 1 THEN max(x.created) END AS created
  FROM stream_stats x JOIN epochs e USING (epoch)
  GROUP BY x.epoch, x.account, x.stream)
SELECT ` + rowText("epoch", "account", "stream", "bytes", "messages", "replica_bytes", "seq", "created",
	"lag(epoch) OVER w", "lag(seq) OVER w", "lag(created) OVER w") + `
FROM s WINDOW w AS (PARTITION BY account, stream ORDER BY epoch)
ORDER BY epoch, account, stream`

var storedStreamSamples = `
SELECT ` + rowText("epoch", "account", "stream", "bytes", "messages", "replica_bytes", "seq", "created",
	"prev_epoch", "prev_seq", "prev_created") + `
FROM stream_samples ORDER BY epoch, account, stream`

// rowText is a SQL expression joining the expressions' text with spaces,
// NULL as "null".
func rowText(exprs ...string) string {
	parts := make([]string, len(exprs))
	for i, e := range exprs {
		parts[i] = "coalesce(CAST(" + e + " AS VARCHAR), 'null')"
	}
	return strings.Join(parts, " || ' ' || ")
}

func stringRows(t *testing.T, s *Store, q string) []string {
	t.Helper()
	rows, err := s.DB().Query(q)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func checkStreamSamples(t *testing.T, s *Store, stage string) {
	t.Helper()
	got, want := stringRows(t, s, storedStreamSamples), stringRows(t, s, referenceStreamSamples)
	if len(want) == 0 {
		t.Fatalf("%s: no stream reports", stage)
	}
	if !equalRows(got, want) {
		t.Errorf("%s: stream_samples differ from the reports (%d rows, want %d)", stage, len(got), len(want))
		for i := range max(len(got), len(want)) {
			var g, w string
			if i < len(got) {
				g = got[i]
			}
			if i < len(want) {
				w = want[i]
			}
			if g != w {
				t.Errorf("row %d:\n got %s\nwant %s", i, g, w)
			}
		}
	}
	if n := count(t, s.DB(), `SELECT count(*) FROM epochs WHERE epoch NOT IN (SELECT epoch FROM stream_sample_epochs)`); n != 0 {
		t.Errorf("%s: %d epochs without samples", stage, n)
	}
	head := rowText("account", "stream", "epoch", "seq", "created")
	heads := stringRows(t, s, `SELECT `+head+` FROM stream_sample_heads ORDER BY 1`)
	newest := stringRows(t, s, `SELECT `+head+` FROM stream_samples
QUALIFY row_number() OVER (PARTITION BY account, stream ORDER BY epoch DESC) = 1 ORDER BY 1`)
	if !equalRows(heads, newest) {
		t.Errorf("%s: heads %v, want each stream's newest sample %v", stage, heads, newest)
	}
}

// addVariedEpochs appends epochs from..to as copies of epoch 1's reports,
// without samples, as a writer outside WriteSnapshot or a release before
// the samples leaves them: sequences that grow, fall back (a reset) and
// stay; a stream missing from some epochs, so that its previous sample is
// several epochs back; creation times that are unknown or disagree; no
// leader; and one server's reports missing.
func addVariedEpochs(t *testing.T, s *Store, from, to int) {
	t.Helper()
	for _, q := range []string{
		`INSERT INTO epochs SELECT t.* REPLACE (i AS epoch, t.started_at + to_seconds(30 * (i - 1)) AS started_at)
FROM epochs t, range(?, ? + 1) r(i) WHERE t.epoch = 1`,
		`INSERT INTO stream_stats SELECT t.* REPLACE (i AS epoch,
    CASE WHEN i % 11 = 0 THEN 3 ELSE last_seq + 7 * i END AS last_seq,
    bytes + i AS bytes, messages + 2 * i AS messages,
    CASE WHEN i % 13 = 0 THEN NULL WHEN i % 17 = 0 AND server_id = 'NFAKESERVER1ID' THEN created + INTERVAL 1 SECOND ELSE created END AS created,
    CASE WHEN i % 5 = 0 THEN false ELSE is_leader END AS is_leader)
FROM stream_stats t, range(?, ? + 1) r(i)
WHERE t.epoch = 1 AND NOT (i % 3 = 0 AND hash(t.stream) % 2 = 0) AND NOT (i % 7 = 0 AND t.server_id = 'NFAKESERVER0ID')`,
	} {
		if _, err := s.DB().Exec(q, from, to); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}

// Each epoch's write stores its stream samples, linked to each stream's
// previous sample; the fill gives epochs without samples the same rows the
// reports give, continuing from the written ones.
func TestStreamSamplesMatchReports(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	for i := range 3 {
		if _, err := s.WriteSnapshot(ctx, fixtureSnapshot(t, t0.Add(time.Duration(i)*30*time.Second))); err != nil {
			t.Fatal(err)
		}
	}
	if n := count(t, s.DB(), `SELECT count(*) FROM stream_samples WHERE epoch = 3 AND prev_epoch = 2`); n == 0 {
		t.Fatal("epoch 3 has no samples linked to epoch 2")
	}
	checkStreamSamples(t, s, "written")
	addVariedEpochs(t, s, 4, 60)
	n, err := s.FillStreamSamples(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 57 {
		t.Errorf("filled %d epochs, want 57", n)
	}
	checkStreamSamples(t, s, "filled")
	// The variations reach the rows: a stream without a sample in the
	// epoch before, unknown creation and a fall in the sequence.
	for q, what := range map[string]string{
		`SELECT count(*) FROM stream_samples WHERE prev_epoch < epoch - 1`: "a previous sample more than one epoch back",
		`SELECT count(*) FROM stream_samples WHERE created IS NULL`:        "an unknown creation time",
		`SELECT count(*) FROM stream_samples WHERE seq < prev_seq`:         "a sequence reset",
	} {
		if count(t, s.DB(), q) == 0 {
			t.Errorf("no sample has %s", what)
		}
	}
}

// An epoch written while an earlier one has no samples gets none, so that
// its previous samples are never taken from an incomplete history; the
// fill then writes both in order. Reopening fills nothing by itself.
func TestStreamSamplesWaitForEarlierEpochs(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "db.duckdb")
	s, err := OpenPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		if _, err := s.WriteSnapshot(ctx, fixtureSnapshot(t, t0.Add(time.Duration(i)*30*time.Second))); err != nil {
			t.Fatal(err)
		}
	}
	// Epoch 2 as a release before the samples left it.
	for _, q := range []string{`DELETE FROM stream_samples WHERE epoch = 2`, `DELETE FROM stream_sample_epochs WHERE epoch = 2`,
		`DELETE FROM stream_sample_heads`, `INSERT INTO stream_sample_heads SELECT account, stream, epoch, seq, created FROM stream_samples`} {
		if _, err := s.DB().Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if s, err = OpenPath(ctx, path); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.WriteSnapshot(ctx, fixtureSnapshot(t, t0.Add(time.Minute))); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s.DB(), `SELECT count(*) FROM stream_samples WHERE epoch >= 2`); n != 0 {
		t.Errorf("%d samples written after an epoch without them", n)
	}
	if n := count(t, s.DB(), `SELECT count(*) FROM stream_sample_epochs WHERE epoch >= 2`); n != 0 {
		t.Errorf("%d epochs after one without samples marked as sampled", n)
	}
	n, err := s.FillStreamSamples(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("filled %d epochs, want 2", n)
	}
	checkStreamSamples(t, s, "filled after a gap")
	if _, err := s.WriteSnapshot(ctx, fixtureSnapshot(t, t0.Add(90*time.Second))); err != nil {
		t.Fatal(err)
	}
	checkStreamSamples(t, s, "written after the fill")
}

// The fill covers every epoch without samples across several batches,
// releasing the write lock between them, and a second fill finds nothing.
func TestStreamSamplesFillInBatches(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	if _, err := s.WriteSnapshot(ctx, fixtureSnapshot(t, t0)); err != nil {
		t.Fatal(err)
	}
	n := 2*streamSampleBatch + 17
	addVariedEpochs(t, s, 2, n)
	batches := 0
	beforeStreamSampleBatch = func() {
		batches++
		if len(s.mu.token()) != 1 || s.mu.op != opStreamSamples {
			t.Errorf("a fill batch runs without holding the write lock as %s", opStreamSamples)
		}
	}
	t.Cleanup(func() { beforeStreamSampleBatch = nil })
	filled, err := s.FillStreamSamples(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if filled != int64(n-1) {
		t.Errorf("filled %d epochs, want %d", filled, n-1)
	}
	// The third batch is short of a full one, so the fill stops there.
	if batches != 3 {
		t.Errorf("%d batches, want 3", batches)
	}
	checkStreamSamples(t, s, "filled in batches")
	if filled, err = s.FillStreamSamples(ctx); err != nil || filled != 0 {
		t.Errorf("a second fill filled %d (%v), want none", filled, err)
	}
}

// The retention sweep deletes the samples, the sampled marks and the heads
// of the epochs it deletes.
func TestStreamSamplesSwept(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	for i := range 3 {
		if _, err := s.WriteSnapshot(ctx, fixtureSnapshot(t, t0.Add(time.Duration(i)*time.Hour))); err != nil {
			t.Fatal(err)
		}
	}
	// A stream last seen in epoch 1 keeps its head there until the sweep.
	if _, err := s.DB().Exec(`INSERT INTO stream_sample_heads VALUES ('GONE', 'old', 1, 5, NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Sweep(ctx, t0.Add(2*time.Hour+time.Minute), 90*time.Minute); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"stream_samples", "stream_sample_epochs", "stream_sample_heads"} {
		if n := count(t, s.DB(), `SELECT count(*) FROM `+table+` WHERE epoch NOT IN (SELECT epoch FROM epochs)`); n != 0 {
			t.Errorf("%d rows of swept epochs left in %s", n, table)
		}
	}
	if n := count(t, s.DB(), `SELECT count(*) FROM stream_sample_epochs`); n != 2 {
		t.Errorf("%d sampled epochs after the sweep, want 2", n)
	}
}

// A fill whose time is up still fills one batch, so each call makes
// progress, and reports that epochs remain only when they do; one with
// time left fills them all.
func TestFillsStopWhenTheirTimeIsUp(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	if _, err := s.WriteSnapshot(ctx, fixtureSnapshot(t, t0)); err != nil {
		t.Fatal(err)
	}
	n := streamSampleBatch + 10
	addVariedEpochs(t, s, 2, n)
	past := time.Now().Add(-time.Second)
	for _, f := range []struct {
		what string
		fill func(context.Context, time.Time) (int64, bool, error)
	}{{"stream samples", s.FillStreamSamplesUntil}, {"asset counts", s.FillAssetCountsUntil}} {
		if got, more, err := f.fill(ctx, past); got != streamSampleBatch || !more || err != nil {
			t.Errorf("%s past their time: %d filled, more %v, %v; want one batch (%d) and more", f.what, got, more, err, streamSampleBatch)
		}
		if got, more, err := f.fill(ctx, past); got != int64(n-1-streamSampleBatch) || more || err != nil {
			t.Errorf("%s past their time again: %d filled, more %v, %v; want the last %d and none left", f.what, got, more, err, n-1-streamSampleBatch)
		}
		if got, more, err := f.fill(ctx, past); got != 0 || more || err != nil {
			t.Errorf("%s with none missing: %d filled, more %v, %v; want none and none left", f.what, got, more, err)
		}
	}
	if assetCountBatch != streamSampleBatch {
		t.Fatalf("the batches differ: %d asset counts, %d stream samples", assetCountBatch, streamSampleBatch)
	}
}
