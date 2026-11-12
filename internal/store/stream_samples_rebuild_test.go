package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/archive"
	"github.com/shadone/nats-lens/internal/store"
)

// The archive holds no stream samples; a rebuild computes them again from
// the loaded reports, in epoch order, with the same previous samples.
func TestRebuildComputesStreamSamples(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, at := range []time.Time{h0.Add(10 * time.Minute), h0.Add(40 * time.Minute), h1.Add(5 * time.Minute)} {
		if _, err := st.WriteSnapshot(ctx, store.FixtureSnapshot(t, at)); err != nil {
			t.Fatal(err)
		}
	}
	const samples = `SELECT list(concat_ws(' ', epoch, account, stream, coalesce(bytes::VARCHAR, '-'), coalesce(messages::VARCHAR, '-'),
    coalesce(replica_bytes::VARCHAR, '-'), coalesce(seq::VARCHAR, '-'), coalesce(created::VARCHAR, '-'),
    coalesce(prev_epoch::VARCHAR, '-'), coalesce(prev_seq::VARCHAR, '-'), coalesce(prev_created::VARCHAR, '-'))
    ORDER BY epoch, account, stream)::VARCHAR FROM stream_samples`
	const marks = `SELECT list(epoch ORDER BY epoch)::VARCHAR FROM stream_sample_epochs`
	const heads = `SELECT list(concat_ws(' ', account, stream, epoch) ORDER BY account, stream)::VARCHAR FROM stream_sample_heads`
	want, wantMarks, wantHeads := scalar[string](t, st, samples), scalar[string](t, st, marks), scalar[string](t, st, heads)
	if scalar[int](t, st, `SELECT count(*) FROM stream_samples WHERE prev_epoch IS NOT NULL`) == 0 {
		t.Fatalf("written samples have no previous sample: %s", want)
	}
	archiveDir := t.TempDir()
	if n, err := newArchiver(st, archiveDir).Export(ctx, now); err != nil || n != 2 {
		t.Fatalf("exported %d hours, err %v; want h0 and h1", n, err)
	}
	for _, table := range []string{"stream_samples/", "stream_sample_heads/", "stream_sample_epochs/"} {
		if files, _ := (archive.LocalDir{Dir: archiveDir}).List(ctx, table); len(files) != 0 {
			t.Errorf("%s archived: %v", table, files)
		}
	}
	rb, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer rb.Close()
	if _, err := rb.DB().Exec(`SET GLOBAL TimeZone = 'Pacific/Honolulu'`); err != nil {
		t.Fatal(err)
	}
	if err := newArchiver(rb, archiveDir).Rebuild(ctx, now); err != nil {
		t.Fatal(err)
	}
	if got := scalar[string](t, rb, samples); got != want {
		t.Errorf("rebuilt samples\n%s\nwant\n%s", got, want)
	}
	if got := scalar[string](t, rb, marks); got != wantMarks {
		t.Errorf("rebuilt sampled epochs %s, want %s", got, wantMarks)
	}
	if got := scalar[string](t, rb, heads); got != wantHeads {
		t.Errorf("rebuilt heads %s, want %s", got, wantHeads)
	}
}
