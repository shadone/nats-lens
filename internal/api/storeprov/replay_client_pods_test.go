package storeprov_test

import (
	"slices"
	"testing"

	"github.com/shadone/nats-lens/internal/api"
)

// A client pod watch's collector.k8s_gap makes no Kubernetes
// gap in Replay; a NATS pod watch's gap in the same window still does.
func TestReplayLeavesOutClientPodWatchGaps(t *testing.T) {
	p, r := replayStore(t)
	r.add("events", map[string]any{"id": 950, "time": rEpoch(5), "received_at": rEpoch(8), "source": "collector", "kind": "collector.k8s_gap",
		"summary": "gap", "raw": `{"source":"client pods in shop","error":"Kubernetes API returned HTTP 403"}`})
	f, err := p.Replay(ctx, replayReq(rEpoch(3), rEpoch(40)))
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(f.Gaps, func(g api.ReplayGap) bool { return g.Kind == api.GapKubernetes }) {
		t.Errorf("client pod watch gap in Replay: %+v", f.Gaps)
	}
	r.add("events", map[string]any{"id": 951, "time": rEpoch(6), "received_at": rEpoch(9), "source": "collector", "kind": "collector.k8s_gap",
		"summary": "gap", "raw": `{"source":"pods in nats","error":"Kubernetes API returned HTTP 403"}`})
	f, err = p.Replay(ctx, replayReq(rEpoch(3), rEpoch(40)))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, g := range f.Gaps {
		if g.Kind == api.GapKubernetes {
			n++
			if !g.Start.Equal(rEpoch(6)) {
				t.Errorf("kubernetes gap %+v", g)
			}
		}
	}
	if n != 1 {
		t.Errorf("kubernetes gaps %d, want the NATS pod watch's: %+v", n, f.Gaps)
	}
}
