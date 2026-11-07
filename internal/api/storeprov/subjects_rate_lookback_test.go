package storeprov_test

import (
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// apiRateEpochs builds n 30 s epochs ending at 14:00 numbered num(i), skipping the
// indexes in skip, with pts (indexes, not numbers), reads APP's API row.
func apiRateEpochs(t *testing.T, n int64, num func(int64) int64, skip map[int64]bool, pts []epochPt, rng *v1.TimeRange) *v1.SubjectRow {
	t.Helper()
	last := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	st := emptyStore(t)
	db := st.DB()
	ex := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	for i := int64(1); i <= n; i++ {
		if skip[i] {
			continue
		}
		at := epochAt(n, i)
		ex(`INSERT INTO epochs VALUES (?, ?, ?, 0, 9, 9, [], [], [], 0, false, [], NULL)`, num(i), at, at.Add(time.Second))
	}
	for _, p := range pts {
		if skip[p.e] {
			continue
		}
		ex(`INSERT INTO account_stats (epoch, server_id, account, js_streams, js_api_total, js_api_errors) VALUES (?, ?, ?, 1, ?, 0)`, num(p.e), p.s, p.a, p.v)
	}
	p := &storeprov.Provider{St: st, Interval: 30 * time.Second, Live: true, Clock: func() time.Time { return last.Add(10 * time.Second) }, Retention: 14 * day}
	_, c := apitest.Serve(t, p)
	res := must(c.GetSubjects(ctx, connect.NewRequest(&v1.GetSubjectsRequest{Range: rng})))
	return subjRow(res, "APP", api.JetStreamAPIPattern)
}

func identity(i int64) int64 { return i }

// returning is S1: a report at prev, then a local 0 at first, then the cluster total.
func returning(prev, first, end int64) []epochPt {
	pts := []epochPt{{prev, "S1", "APP", 0}, {first, "S1", "APP", 0}}
	for e := first + 1; e <= end; e++ {
		pts = append(pts, epochPt{e, "S1", "APP", 1_000_000 + 90*e})
	}
	return pts
}

// lookN is the epochs of the look-back cases: 2.5 hours of 30-second epochs.
const lookN = 300

// S1 returns 61 minutes after its last report with a local 0 at the
// window's start: no report in the hour before, so that 0 is left out.
func TestSubjectsAPIRateLookBackGap61(t *testing.T) {
	pts := append(steady30(lookN), returning(lookN-151, lookN-29, lookN)...)
	js := apiRateEpochs(t, lookN, identity, nil, pts, nil)
	if js.GetApiPerSecond() != 3 {
		t.Errorf("gap 61 min: rate %v, want 3 (first window report dropped)", js.GetApiPerSecond())
	}
}

// S1 returns 59 minutes after its last report: a report in the hour
// before, so the local 0 is kept and its jump reads high (the cost of a
// bounded look-back).
func TestSubjectsAPIRateLookBackGap59(t *testing.T) {
	pts := append(steady30(lookN), returning(lookN-147, lookN-29, lookN)...)
	js := apiRateEpochs(t, lookN, identity, nil, pts, nil)
	if js.GetApiPerSecond() < 1000 {
		t.Errorf("gap 59 min: rate %v, want the kept local 0 to show as a jump", js.GetApiPerSecond())
	}
}

// A series whose first report ever is the window's first.
func TestSubjectsAPIRateLookBackFirstEverAtWindowStart(t *testing.T) {
	pts := append(steady30(lookN), epochPt{lookN - 29, "S1", "APP", 0})
	for e := int64(lookN - 28); e <= lookN; e++ {
		pts = append(pts, epochPt{e, "S1", "APP", 1_000_000 + 90*e})
	}
	js := apiRateEpochs(t, lookN, identity, nil, pts, nil)
	if js.GetApiPerSecond() != 3 {
		t.Errorf("rate %v", js.GetApiPerSecond())
	}
}

// Gap61 for a past range.
func TestSubjectsAPIRateLookBackPastRange(t *testing.T) {
	const endIdx = 200
	pts := append(steady30(lookN), returning(endIdx-151, endIdx-29, lookN)...)
	rng := &v1.TimeRange{Start: timestamppb.New(epochAt(lookN, endIdx).Add(-24 * time.Hour)), End: timestamppb.New(epochAt(lookN, endIdx).Add(10 * time.Second))}
	js := apiRateEpochs(t, lookN, identity, nil, pts, rng)
	if js.GetApiPerSecond() != 3 {
		t.Errorf("past range: rate %v", js.GetApiPerSecond())
	}
}

// Reports every other epoch.
func TestSubjectsAPIRateLookBackEveryOther(t *testing.T) {
	var pts []epochPt
	for e := int64(2); e <= lookN; e += 2 {
		pts = append(pts, epochPt{e, "S0", "APP", 1_000_000 + 90*e})
	}
	js := apiRateEpochs(t, lookN, identity, nil, pts, nil)
	if js.GetApiPerSecond() != 3 {
		t.Errorf("rate %v", js.GetApiPerSecond())
	}
	// S1 every other epoch, returning after 61 min on odd epochs.
	pts = append(steady30(lookN), returning(lookN-151, lookN-29, lookN)...)
	js = apiRateEpochs(t, lookN, identity, nil, pts, nil)
	if js.GetApiPerSecond() != 3 {
		t.Errorf("rate %v", js.GetApiPerSecond())
	}
}

// No epochs in most of the hour before: the collector stopped.
func TestSubjectsAPIRateLookBackCollectorGapInHour(t *testing.T) {
	skip := map[int64]bool{}
	for i := int64(lookN - 149); i <= lookN-30; i++ {
		skip[i] = true
	}
	// S1 reports before the gap (outside the hour) and returns with a local 0.
	pts := append(steady30(lookN), returning(lookN-160, lookN-29, lookN)...)
	js := apiRateEpochs(t, lookN, identity, skip, pts, nil)
	if js.GetApiPerSecond() != 3 {
		t.Errorf("rate %v", js.GetApiPerSecond())
	}
}

// Epoch numbers ten apart: the look-back goes by time, not by count.
func TestSubjectsAPIRateLookBackSparseNumbers(t *testing.T) {
	ten := func(i int64) int64 { return i * 10 }
	pts := append(steady30(lookN), returning(lookN-151, lookN-29, lookN)...)
	if js := apiRateEpochs(t, lookN, ten, nil, pts, nil); js.GetApiPerSecond() != 3 {
		t.Errorf("gap 61 sparse: rate %v", js.GetApiPerSecond())
	}
	pts = append(steady30(lookN), returning(lookN-147, lookN-29, lookN)...)
	if js := apiRateEpochs(t, lookN, ten, nil, pts, nil); js.GetApiPerSecond() < 1000 {
		t.Errorf("gap 59 sparse: rate %v", js.GetApiPerSecond())
	}
}
