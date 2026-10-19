package api

import (
	"cmp"
	"slices"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// SetDiffers fills each cell's differs: the settings on which its report
// differs from the value most cells report; on a tie the leader's value
// wins when it is among the most common, else the smallest. A cell that
// reports no value for a setting never differs on it. Both providers call
// it, so the matrix marks the same cells.
func SetDiffers(cells []*v1.PlacementCell, leader string) {
	settings := []struct {
		name string
		of   func(*v1.PlacementCell) string
	}{
		{"created", func(c *v1.PlacementCell) string {
			if c.GetCreated() == nil || c.GetCreated().AsTime().Unix() <= 0 {
				return ""
			}
			return c.GetCreated().AsTime().UTC().Format(time.RFC3339Nano)
		}},
		{"config_hash", (*v1.PlacementCell).GetConfigHash},
	}
	for _, c := range cells {
		c.Differs = nil
	}
	for _, s := range settings {
		lead := ""
		for _, c := range cells {
			if leader != "" && c.GetServer() == leader {
				lead = s.of(c)
			}
		}
		ref := mostCommon(cells, s.of, lead)
		if ref == "" {
			continue
		}
		for _, c := range cells {
			if v := s.of(c); v != "" && v != ref {
				c.Differs = append(c.Differs, s.name)
			}
		}
	}
}

// mostCommon is the non-empty value most cells report; on a tie, prefer
// when it is among the most common, else the smallest. Empty when no cell
// reports one.
func mostCommon(cells []*v1.PlacementCell, of func(*v1.PlacementCell) string, prefer string) string {
	n := map[string]int{}
	for _, c := range cells {
		if v := of(c); v != "" {
			n[v]++
		}
	}
	top := 0
	for _, k := range n {
		top = max(top, k)
	}
	if prefer != "" && n[prefer] == top {
		return prefer
	}
	best := ""
	for v, k := range n {
		if k == top && (best == "" || v < best) {
			best = v
		}
	}
	return best
}

// placementMatches reports whether an asset passes a filter.
func placementMatches(a *v1.PlacementAsset, f v1.PlacementFilter) bool {
	has := func(st v1.PlacementState) bool {
		return slices.ContainsFunc(a.GetCells(), func(c *v1.PlacementCell) bool { return c.GetState() == st })
	}
	switch f {
	case v1.PlacementFilter_PLACEMENT_FILTER_SERVERS_DISAGREE:
		return a.GetServersDisagree() || has(v1.PlacementState_PLACEMENT_STATE_DISAGREES)
	case v1.PlacementFilter_PLACEMENT_FILTER_REPLICA_BEHIND:
		return has(v1.PlacementState_PLACEMENT_STATE_BEHIND)
	case v1.PlacementFilter_PLACEMENT_FILTER_MISSING:
		return a.GetUnplaced() > 0 || has(v1.PlacementState_PLACEMENT_STATE_MISSING)
	case v1.PlacementFilter_PLACEMENT_FILTER_SINGLE_REPLICA:
		if a.Replicas != nil {
			return a.GetReplicas() == 1
		}
		return len(a.GetCells()) == 1
	}
	return true
}

// FinishPlacement sorts every stream and its consumers by key, counts the
// assets each filter lists and keeps the streams that pass the filter or
// have consumers that do, each with only the consumers that pass.
func FinishPlacement(res *v1.GetPlacementResponse, f v1.PlacementFilter) *v1.GetPlacementResponse {
	byKey := func(a, b *v1.PlacementAsset) int { return cmp.Compare(a.GetEntity().GetKey(), b.GetEntity().GetKey()) }
	slices.SortFunc(res.Streams, byKey)
	counts := &v1.PlacementCounts{}
	count := func(a *v1.PlacementAsset) {
		counts.All++
		for f, n := range map[v1.PlacementFilter]*int32{
			v1.PlacementFilter_PLACEMENT_FILTER_SERVERS_DISAGREE: &counts.ServersDisagree,
			v1.PlacementFilter_PLACEMENT_FILTER_REPLICA_BEHIND:   &counts.ReplicaBehind,
			v1.PlacementFilter_PLACEMENT_FILTER_MISSING:          &counts.Missing,
			v1.PlacementFilter_PLACEMENT_FILTER_SINGLE_REPLICA:   &counts.SingleReplica,
		} {
			if placementMatches(a, f) {
				*n++
			}
		}
	}
	var kept []*v1.PlacementAsset
	for _, s := range res.GetStreams() {
		slices.SortFunc(s.Consumers, byKey)
		count(s)
		var cons []*v1.PlacementAsset
		for _, c := range s.GetConsumers() {
			count(c)
			if c.Matches = placementMatches(c, f); c.Matches {
				cons = append(cons, c)
			}
		}
		s.Matches, s.Consumers = placementMatches(s, f), cons
		if s.Matches || len(cons) > 0 {
			kept = append(kept, s)
		}
	}
	res.Streams, res.Counts = kept, counts
	return res
}
