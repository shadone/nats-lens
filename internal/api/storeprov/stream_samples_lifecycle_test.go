package storeprov

import (
	"context"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/store"
)

// openSampleStore opens an empty store in Pacific/Honolulu.
func openSampleStore(t *testing.T) *store.Store {
	t.Helper()
	t.Setenv("TZ", "Pacific/Honolulu")
	st, err := store.OpenPath(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if _, err := st.DB().Exec(`SET GLOBAL TimeZone = 'Pacific/Honolulu'`); err != nil {
		t.Fatal(err)
	}
	return st
}

// writeSampleEpochs writes the sample store's epochs from to before to.
func writeSampleEpochs(t *testing.T, st *store.Store, from, to int) {
	t.Helper()
	for e := from; e < to; e++ {
		if _, err := st.WriteSnapshot(context.Background(), sampleSnapshot(e)); err != nil {
			t.Fatalf("epoch %d: %v", e, err)
		}
	}
}

// sampleRowsHash counts stream_samples and hashes every row.
const sampleRowsHash = `SELECT count(*), coalesce(bit_xor(hash(epoch, account, stream, bytes, messages, replica_bytes, seq, created,
  prev_epoch, prev_seq, prev_created)), 0) FROM stream_samples`

// A sweep that removes a stream's head while the stream is absent, then
// the stream comes back: the samples still read as the reports.
func TestStreamSamplesAfterASweepRemovedAHead(t *testing.T) {
	st := openSampleStore(t)
	writeSampleEpochs(t, st, 0, 48)
	res, err := st.Sweep(context.Background(), sampleAt(47), 0)
	if err != nil {
		t.Fatal(err)
	}
	var heads int
	if err := st.DB().QueryRow(`SELECT count(*) FROM stream_sample_heads WHERE stream = 'recreated'`).Scan(&heads); err != nil {
		t.Fatal(err)
	}
	t.Logf("cutoff %d, swept %d epochs, recreated heads %d", res.Cutoff, res.Epochs, heads)
	if heads != 0 {
		t.Fatalf("recreated's head survived the sweep")
	}
	writeSampleEpochs(t, st, 48, sampleEpochs)
	var nulls int
	if err := st.DB().QueryRow(`SELECT count(*) FROM stream_samples WHERE stream = 'recreated' AND prev_epoch IS NULL`).Scan(&nulls); err != nil {
		t.Fatal(err)
	}
	t.Logf("recreated samples without a previous one: %d", nulls)
	got := readSamples(t, st)
	unsample(t, st, sampleT0.Add(-24*time.Hour))
	want := readSamples(t, st)
	compareSampleReads(t, got, want)
}

// Epochs written while earlier ones lack samples get none; the fill
// later writes exactly the rows a store written in one go has.
func TestStreamSamplesWrittenWhileUnfilled(t *testing.T) {
	ref := openSampleStore(t)
	writeSampleEpochs(t, ref, 0, sampleEpochs)
	var wantRows int
	var wantHash uint64
	if err := ref.DB().QueryRow(sampleRowsHash).Scan(&wantRows, &wantHash); err != nil {
		t.Fatal(err)
	}

	st := openSampleStore(t)
	writeSampleEpochs(t, st, 0, 40)
	unsample(t, st, sampleAt(20))
	writeSampleEpochs(t, st, 40, sampleEpochs)
	var marked int
	if err := st.DB().QueryRow(`SELECT count(*) FROM stream_sample_epochs`).Scan(&marked); err != nil {
		t.Fatal(err)
	}
	if marked != 20 {
		t.Fatalf("%d epochs marked, want 20", marked)
	}
	// A range over the sampled part reads samples, one over the rest the
	// reports; both give the reports' answers.
	got := readSamples(t, st)
	n, err := st.FillStreamSamples(context.Background())
	if err != nil || n != sampleEpochs-20 {
		t.Fatalf("filled %d (%v)", n, err)
	}
	var rows int
	var hash uint64
	if err := st.DB().QueryRow(sampleRowsHash).Scan(&rows, &hash); err != nil {
		t.Fatal(err)
	}
	if rows != wantRows || hash != wantHash {
		t.Fatalf("after the fill %d rows hash %d, want %d hash %d", rows, hash, wantRows, wantHash)
	}
	want := readSamples(t, st)
	compareSampleReads(t, got, want)
}
