package api

import (
	"strings"
	"testing"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

func TestReplayClientChangeContextAvailability(t *testing.T) {
	at := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name     string
		captured bool
		gaps     []ReplayGap
		cut      bool
		want     string
	}{
		{name: "no events", want: "no events captured"},
		{name: "collector restart", captured: true, gaps: []ReplayGap{{Kind: GapCollector, Start: at.Add(time.Minute), End: at.Add(2 * time.Minute)}}, want: "collector downtime"},
		{name: "source loss", captured: true, gaps: []ReplayGap{{Kind: GapEvents, Start: at.Add(time.Minute), End: at.Add(2 * time.Minute)}}, want: "event connection lost"},
		{name: "read cut", captured: true, cut: true, want: "finding read was truncated"},
		{name: "observed zero", captured: true, want: "0 reconnect pairs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &ReplayFacts{A: at, B: at.Add(time.Hour), EpochA: 1, EpochB: 2, EpochAAt: at, EpochBAt: at.Add(time.Hour), Collected: true, EventsCaptured: tc.captured, Gaps: tc.gaps, TransitionsTruncated: tc.cut, TransitionsReadUntil: at.Add(time.Minute),
				ClientLibraries: &ReplayClientLibraries{Rows: []ReplayClientLibrary{{Epoch: 1, App: "checkout", Account: "APP", Lang: "go", Version: "1", Connections: 2}, {Epoch: 2, App: "checkout", Account: "APP", Lang: "go", Version: "2", Connections: 3}}}}
			res := FinishReplay(f)
			var step *v1.ReplayStep
			for _, s := range res.Steps {
				if strings.Contains(s.Title, "checkout client-library") {
					step = s
				}
			}
			if step == nil || !strings.Contains(step.Body, tc.want) {
				t.Fatalf("want %q in %v", tc.want, step)
			}
			if !tc.captured || len(tc.gaps) > 0 {
				if res.Counts.ClientReconnects != nil || strings.Contains(step.Body, "0 reconnect pairs") {
					t.Fatal("unknown was reported as zero")
				}
			}
		})
	}
}
