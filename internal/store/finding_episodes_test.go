package store

import (
	"context"
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
