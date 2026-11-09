package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// Unlike FindingsAt's compatibility projection, occurrence reads must not
// collapse a resolved firing and a later recurrence into the same row.
func TestFindingEpisodesPreserveRepeatedOccurrences(t *testing.T) {
	s := refireStore(t)
	ctx := context.Background()
	q := FindingEpisodeQuery{From: t0, To: t0.Add(6 * time.Minute), Limit: 50}
	page, err := s.FindingEpisodes(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Items) != 2 {
		t.Fatalf("episodes = %+v; want both retained occurrences", page)
	}
	byStart := map[int64]FindingEpisode{}
	for _, e := range page.Items {
		byStart[e.Finding.SinceEpoch] = e
	}
	first, ok := byStart[2]
	if !ok || !first.StartKnown || first.Finding.Detail != "first" || first.Finding.Severity != "warning" || first.Finding.Firing() || !first.Finding.ResolvedAt.Equal(t0.Add(3*time.Minute)) {
		t.Fatalf("first occurrence replaced by latest state: %+v", first)
	}
	second, ok := byStart[5]
	if !ok || !second.StartKnown || second.Finding.Detail != "second" || !second.Finding.Firing() {
		t.Fatalf("second occurrence: %+v", second)
	}
	if first.Finding.RelatedConfig != nil || first.Finding.ConditionSince != nil {
		t.Fatalf("current-only metadata presented as historical: %+v", first.Finding)
	}
	// Explicit selection remains stable after another firing on the same ID.
	addEpoch(t, s, 6, t0.Add(6*time.Minute))
	apply(t, s, 6, okRun("stream-001"))
	addEpoch(t, s, 7, t0.Add(7*time.Minute))
	apply(t, s, 7, okRun("stream-001", frow("A/s", "info", "third")))
	q.FindingID = FindingID("stream-001", "A/s")
	q.StartEpoch = 2
	q.To = t0.Add(8 * time.Minute)
	again, err := s.FindingEpisodes(ctx, q)
	if err != nil || len(again.Items) != 1 || again.Items[0].Finding.Detail != "first" || again.Items[0].Finding.SinceEpoch != 2 {
		t.Fatalf("stable episode lookup = %+v, %v", again, err)
	}
}

func TestFindingEpisodesHalfOpenOverlapAndPaging(t *testing.T) {
	s := refireStore(t)
	ctx := context.Background()
	read := func(from, to time.Duration, offset int) FindingEpisodePage {
		t.Helper()
		p, err := s.FindingEpisodes(ctx, FindingEpisodeQuery{From: t0.Add(from), To: t0.Add(to), Limit: 1, Offset: offset})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	if p := read(3*time.Minute, 5*time.Minute, 0); p.Total != 0 || len(p.Items) != 0 {
		t.Fatalf("nonoverlapping half-open range: %+v", p)
	}
	a, b := read(0, 6*time.Minute, 0), read(0, 6*time.Minute, 1)
	if a.Total != 2 || b.Total != 2 || len(a.Items) != 1 || len(b.Items) != 1 || a.Items[0].Finding.SinceEpoch == b.Items[0].Finding.SinceEpoch {
		t.Fatalf("pages: %+v / %+v", a, b)
	}
	// An occurrence already active when the selected range begins remains visible.
	if p := read(150*time.Second, 165*time.Second, 0); p.Total != 1 || len(p.Items) != 1 || p.Items[0].Finding.SinceEpoch != 2 || !p.Items[0].Finding.Firing() {
		t.Fatalf("active at range start: %+v", p)
	}
}

func TestFindingEpisodesSeparateHistoricalAndLatestState(t *testing.T) {
	s := refireStore(t)
	p, err := s.FindingEpisodes(context.Background(), FindingEpisodeQuery{From: t0.Add(150 * time.Second), To: t0.Add(165 * time.Second), Limit: 10})
	if err != nil || len(p.Items) != 1 {
		t.Fatalf("page = %+v, %v", p, err)
	}
	e := p.Items[0]
	if !e.Finding.Firing() || e.Finding.Severity != "warning" || !e.Finding.ResolvedAt.IsZero() {
		t.Fatalf("historical state: %+v", e)
	}
	if !e.ResolutionAt.Equal(t0.Add(3*time.Minute)) || e.ResolutionEpoch != 3 || e.LatestState != StateFiring || e.LatestSeverity != "critical" || e.LatestStartEpoch != 5 {
		t.Fatalf("later evidence not distinguished: %+v", e)
	}
}

func TestFindingEpisodesStreamScopeIncludesChildrenOnly(t *testing.T) {
	s := openMem(t)
	addEpoch(t, s, 1, t0.Add(time.Minute))
	apply(t, s, 1,
		okRun("stream-001", frow("APP/orders", "warning", "parent"), frow("OTHER/orders", "warning", "other account")),
		CheckRun{Code: "consumer-006", EntityType: "consumer", Status: RunOK, Rows: []FindingRow{
			frow("APP/orders/worker", "warning", "child"), frow("APP/orders-extra/worker", "warning", "other stream")}})
	p, err := s.FindingEpisodes(context.Background(), FindingEpisodeQuery{From: t0, To: t0.Add(2 * time.Minute), EntityType: "stream", EntityKey: "APP/orders", Limit: 10})
	if err != nil || p.Total != 2 || len(p.Items) != 2 {
		t.Fatalf("stream scope = %+v, %v", p, err)
	}
	for _, e := range p.Items {
		if e.Finding.Detail != "parent" && e.Finding.Detail != "child" {
			t.Fatalf("leaked scope: %+v", e)
		}
	}
}

func TestFindingEpisodesRetainedFragmentDoesNotInventOnset(t *testing.T) {
	s := refireStore(t)
	if _, err := s.DB().Exec(`DELETE FROM finding_transitions WHERE finding_id=? AND epoch=2`, FindingID("stream-001", "A/s")); err != nil {
		t.Fatal(err)
	}
	p, err := s.FindingEpisodes(context.Background(), FindingEpisodeQuery{From: t0, To: t0.Add(6 * time.Minute), Limit: 10})
	if err != nil || p.Total != 2 {
		t.Fatalf("retained fragments: %+v, %v", p, err)
	}
	found := false
	for _, e := range p.Items {
		if e.FirstRetainedEpoch == 3 {
			found = true
			if e.StartKnown || e.Finding.SinceEpoch != 0 {
				t.Fatalf("invented onset: %+v", e)
			}
		}
	}
	if !found {
		t.Fatalf("lost inspectable resolution fragment: %+v", p)
	}
}

func TestFindingEpisodesFreezeMembershipAcrossPages(t *testing.T) {
	s := refireStore(t)
	ctx := context.Background()
	q := FindingEpisodeQuery{From: t0, To: t0.Add(10 * time.Minute), Limit: 1}
	first, err := s.FindingEpisodes(ctx, q)
	if err != nil || first.ReadEpoch != 5 || len(first.Items) != 1 {
		t.Fatalf("first page: %+v, %v", first, err)
	}
	addEpoch(t, s, 6, t0.Add(6*time.Minute))
	apply(t, s, 6, okRun("stream-001"))
	addEpoch(t, s, 7, t0.Add(7*time.Minute))
	apply(t, s, 7, okRun("stream-001", frow("A/s", "info", "third")))
	q.Offset = 1
	q.ReadEpoch = first.ReadEpoch
	second, err := s.FindingEpisodes(ctx, q)
	if err != nil || second.Total != 2 || len(second.Items) != 1 || second.Items[0].Finding.SinceEpoch != 2 {
		t.Fatalf("new recurrence shifted continuation: %+v, %v", second, err)
	}
}

func TestFindingEpisodesSelectRetainedFragmentAndRejectFutureBoundary(t *testing.T) {
	s := refireStore(t)
	ctx := context.Background()
	if _, err := s.DB().Exec(`DELETE FROM finding_transitions WHERE epoch=2`); err != nil {
		t.Fatal(err)
	}
	q := FindingEpisodeQuery{From: t0, To: t0.Add(6 * time.Minute), Limit: 10, FindingID: FindingID("stream-001", "A/s"), FirstRetainedEpoch: 3}
	p, err := s.FindingEpisodes(ctx, q)
	if err != nil || len(p.Items) != 1 || p.Items[0].FirstRetainedEpoch != 3 || p.Items[0].StartKnown {
		t.Errorf("fragment lookup substituted another occurrence: %+v, %v", p, err)
	}
	q.ReadEpoch = 6
	if _, err := s.FindingEpisodes(ctx, q); !errors.Is(err, ErrInvalidFindingEpisodeQuery) {
		t.Fatalf("future transition boundary accepted: %v", err)
	}
}

func TestFindingEpisodesValidateAndBoundReads(t *testing.T) {
	s := refireStore(t)
	ctx := context.Background()
	base := FindingEpisodeQuery{From: t0, To: t0.Add(6 * time.Minute), Limit: 10}
	for name, change := range map[string]func(*FindingEpisodeQuery){
		"zero limit":          func(q *FindingEpisodeQuery) { q.Limit = 0 },
		"large limit":         func(q *FindingEpisodeQuery) { q.Limit = 101 },
		"negative offset":     func(q *FindingEpisodeQuery) { q.Offset = -1 },
		"negative epoch":      func(q *FindingEpisodeQuery) { q.FirstRetainedEpoch = -1 },
		"unscoped occurrence": func(q *FindingEpisodeQuery) { q.FirstRetainedEpoch = 2 },
		"large identity":      func(q *FindingEpisodeQuery) { q.FindingID = strings.Repeat("x", 4097) },
		"reversed range":      func(q *FindingEpisodeQuery) { q.To = q.From.Add(-time.Second) },
	} {
		t.Run(name, func(t *testing.T) {
			q := base
			change(&q)
			if _, err := s.FindingEpisodes(ctx, q); !errors.Is(err, ErrInvalidFindingEpisodeQuery) {
				t.Fatalf("invalid query accepted: %v", err)
			}
		})
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.FindingEpisodes(canceled, base); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	if _, err := s.DB().Exec(`UPDATE finding_transitions SET detail=? WHERE epoch=2`, strings.Repeat("x", (8<<20)+1)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FindingEpisodes(ctx, FindingEpisodeQuery{From: t0, To: t0.Add(150 * time.Second), Limit: 10}); !errors.Is(err, ErrFindingEpisodeReadLimit) {
		t.Fatalf("byte budget accepted: %v", err)
	}
}

func TestFindingEpisodesRespectSubMicrosecondExclusiveEnd(t *testing.T) {
	s := refireStore(t)
	for _, extra := range []time.Duration{0, time.Nanosecond, time.Microsecond} {
		p, err := s.FindingEpisodes(context.Background(), FindingEpisodeQuery{From: t0, To: t0.Add(2*time.Minute + extra), Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		want := 0
		if extra > 0 {
			want = 1
		}
		if p.Total != want {
			t.Errorf("exclusive end +%v: %d occurrences, want %d", extra, p.Total, want)
		}
	}
}

func TestFindingEpisodesMissingResolutionDoesNotExtendPastRecurrence(t *testing.T) {
	s := refireStore(t)
	// A retained hole is unknown evidence, not proof that the old occurrence
	// remained active after a separately recorded new firing.
	if _, err := s.db.Exec(`DELETE FROM finding_transitions WHERE epoch=3`); err != nil {
		t.Fatal(err)
	}
	p, err := s.FindingEpisodes(context.Background(), FindingEpisodeQuery{From: t0.Add(5 * time.Minute), To: t0.Add(6 * time.Minute), Limit: 50})
	if err != nil || len(p.Items) != 1 || p.Items[0].Finding.SinceEpoch != 5 {
		t.Fatalf("old occurrence borrowed a later recurrence: %+v %v", p, err)
	}
	earlier, err := s.FindingEpisodes(context.Background(), FindingEpisodeQuery{From: t0.Add(2 * time.Minute), To: t0.Add(3 * time.Minute), Limit: 50})
	if err != nil || len(earlier.Items) != 1 || !earlier.Items[0].ResolutionAt.IsZero() || !earlier.Items[0].EndingUnknown {
		t.Fatalf("missing resolution was invented or not disclosed: %+v %v", earlier, err)
	}
}
