package api_test

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// unsetCounts are the names of the unset counts.
func unsetCounts(c *v1.ReplayCounts) []string {
	var out []string
	fs := c.ProtoReflect().Descriptor().Fields()
	for i := range fs.Len() {
		if f := fs.Get(i); !c.ProtoReflect().Has(f) {
			out = append(out, string(f.Name()))
		}
	}
	slices.Sort(out)
	return out
}

// observedFacts are facts under which every count is observed.
func observedFacts() *api.ReplayFacts {
	f := facts()
	f.Collected, f.EventsCaptured, f.GoneRead, f.Kubernetes = true, true, true, true
	f.Nodes = api.ReplayNodes{Upgraded: 1, Drained: 1, Pools: 1, PoolLabels: true}
	return f
}

// Each count left unset has one entry naming why, the first cause that
// applies; a set count has none.
func TestReplayUnobservedNamesEachUnsetCount(t *testing.T) {
	events := []string{"client_reconnects", "leader_elections", "leaders_moved", "meta_leader_changes", "quorum_lost_reports"}
	rows := []string{"leader_elections", "leaders_moved", "meta_leader_changes", "quorum_lost_reports"}
	gone := []string{"assets_gone_with_delete", "assets_unknown_at_b", "consumers_gone_without_delete", "streams_gone_without_delete"}
	nodes := []string{"node_pools_upgraded", "nodes_drained", "nodes_upgraded"}
	join := func(xs ...[]string) []string {
		var out []string
		for _, x := range xs {
			out = append(out, x...)
		}
		slices.Sort(out)
		return slices.Compact(out)
	}
	R := func(r v1.ReplayUnobservedReason) v1.ReplayUnobservedReason { return r }
	cases := []struct {
		name   string
		change func(*api.ReplayFacts)
		want   map[v1.ReplayUnobservedReason][]string
	}{
		{"all observed", func(*api.ReplayFacts) {}, nil},
		{"no collections", func(f *api.ReplayFacts) { f.Collected = false },
			map[v1.ReplayUnobservedReason][]string{R(v1.ReplayUnobservedReason_REPLAY_UNOBSERVED_REASON_NO_COLLECTIONS): {"findings_raised", "servers_restarted", "servers_stopped"}}},
		{"no collection at A", func(f *api.ReplayFacts) { f.EpochA, f.EpochAAt = 0, time.Time{} },
			map[v1.ReplayUnobservedReason][]string{R(v1.ReplayUnobservedReason_REPLAY_UNOBSERVED_REASON_NO_COLLECTION_AT_A): gone}},
		{"no collection at B", func(f *api.ReplayFacts) { f.EpochB, f.EpochBAt = 0, time.Time{} },
			map[v1.ReplayUnobservedReason][]string{R(v1.ReplayUnobservedReason_REPLAY_UNOBSERVED_REASON_NO_COLLECTION_AT_B): join(gone, []string{"servers_stopped"})}},
		// Events come on connections of their own: a time without
		// collections leaves the event counts set.
		{"collections gap", func(f *api.ReplayFacts) {
			f.Gaps = []api.ReplayGap{{Kind: api.GapCollections, Start: rAt(20, 0), End: rAt(30, 0)}}
		}, nil},
		{"events connection lost", func(f *api.ReplayFacts) {
			f.Gaps = []api.ReplayGap{{Kind: api.GapEvents, Start: rAt(20, 0), End: rAt(25, 0), Event: ev(rAt(20, 0), "collector.not_connected", "")}}
		}, map[v1.ReplayUnobservedReason][]string{R(v1.ReplayUnobservedReason_REPLAY_UNOBSERVED_REASON_EVENTS_CONNECTION_LOST): events}},
		{"events not captured", func(f *api.ReplayFacts) { f.EventsCaptured = false },
			map[v1.ReplayUnobservedReason][]string{R(v1.ReplayUnobservedReason_REPLAY_UNOBSERVED_REASON_EVENTS_NOT_CAPTURED): events}},
		{"collector gap", func(f *api.ReplayFacts) {
			f.Gaps = []api.ReplayGap{{Kind: api.GapCollector, Start: rAt(20, 0), End: rAt(30, 0), Event: ev(rAt(20, 0), "collector.gap", "")}}
		}, map[v1.ReplayUnobservedReason][]string{R(v1.ReplayUnobservedReason_REPLAY_UNOBSERVED_REASON_COLLECTOR_GAP): join(events, nodes)}},
		{"read limit before B", func(f *api.ReplayFacts) {
			f.EventsTruncated, f.EventsReadUntil = true, rAt(30, 0)
			f.TransitionsTruncated, f.TransitionsReadUntil = true, rAt(40, 0)
		}, map[v1.ReplayUnobservedReason][]string{R(v1.ReplayUnobservedReason_REPLAY_UNOBSERVED_REASON_READ_LIMIT): join(rows, []string{"findings_raised"})}},
		{"read limit after B", func(f *api.ReplayFacts) {
			f.EventsTruncated, f.EventsReadUntil = true, rAt(90, 0)
		}, nil},
		{"Kubernetes not watched", func(f *api.ReplayFacts) { f.Kubernetes = false },
			map[v1.ReplayUnobservedReason][]string{R(v1.ReplayUnobservedReason_REPLAY_UNOBSERVED_REASON_KUBERNETES_NOT_WATCHED): nodes}},
		{"Kubernetes gap", func(f *api.ReplayFacts) {
			f.Gaps = []api.ReplayGap{{Kind: api.GapKubernetes, Start: rAt(20, 0), Event: ev(rAt(20, 0), "collector.k8s_gap", "")}}
		}, map[v1.ReplayUnobservedReason][]string{R(v1.ReplayUnobservedReason_REPLAY_UNOBSERVED_REASON_KUBERNETES_GAP): nodes}},
		{"unnamed node", func(f *api.ReplayFacts) { f.Nodes.Unnamed = true },
			map[v1.ReplayUnobservedReason][]string{R(v1.ReplayUnobservedReason_REPLAY_UNOBSERVED_REASON_NODE_UNNAMED): nodes}},
		{"no pool labels", func(f *api.ReplayFacts) { f.Nodes.PoolLabels, f.Nodes.Pools = false, 0 },
			map[v1.ReplayUnobservedReason][]string{R(v1.ReplayUnobservedReason_REPLAY_UNOBSERVED_REASON_NO_POOL_LABELS): {"node_pools_upgraded"}}},
		{"gone not read", func(f *api.ReplayFacts) { f.GoneRead = false },
			map[v1.ReplayUnobservedReason][]string{R(v1.ReplayUnobservedReason_REPLAY_UNOBSERVED_REASON_GONE_NOT_READ): gone}},
		// Two causes: the first in FinishReplay's order is named.
		{"no collection at A and events not captured", func(f *api.ReplayFacts) { f.EpochA, f.EpochAAt, f.EventsCaptured = 0, time.Time{}, false },
			map[v1.ReplayUnobservedReason][]string{
				R(v1.ReplayUnobservedReason_REPLAY_UNOBSERVED_REASON_NO_COLLECTION_AT_A):  gone,
				R(v1.ReplayUnobservedReason_REPLAY_UNOBSERVED_REASON_EVENTS_NOT_CAPTURED): events}},
	}
	for _, tc := range cases {
		f := observedFacts()
		tc.change(f)
		res := api.FinishReplay(f)
		got := map[v1.ReplayUnobservedReason][]string{}
		var listed []string
		for _, u := range res.GetUnobserved() {
			got[u.GetReason()] = append(got[u.GetReason()], u.GetCount())
			listed = append(listed, u.GetCount())
		}
		for r := range got {
			slices.Sort(got[r])
		}
		if len(got) == 0 {
			got = nil
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: unobserved %v, want %v", tc.name, got, tc.want)
		}
		slices.Sort(listed)
		if unset := unsetCounts(res.GetCounts()); !slices.Equal(unset, listed) {
			t.Errorf("%s: unset %v, listed %v", tc.name, unset, listed)
		}
	}
}

// A gap behind an unset count gives its span.
func TestReplayUnobservedGivesTheGap(t *testing.T) {
	f := observedFacts()
	f.Gaps = []api.ReplayGap{{Kind: api.GapCollector, Start: rAt(20, 0), End: rAt(30, 0), Event: ev(rAt(20, 0), "collector.gap", "")}}
	for _, u := range api.FinishReplay(f).GetUnobserved() {
		if !u.GetFrom().AsTime().Equal(rAt(20, 0)) || !u.GetTo().AsTime().Equal(rAt(30, 0)) {
			t.Errorf("%v", u)
		}
	}
}

// A read cut after B unsets no count, but steps after B past the cut may
// be missing, so complete_until stays.
func TestReplayCutAfterBKeepsCompleteUntil(t *testing.T) {
	f := observedFacts()
	f.EventsTruncated, f.EventsReadUntil = true, rAt(90, 0)
	res := api.FinishReplay(f)
	if !res.GetCompleteUntil().AsTime().Equal(rAt(90, 0)) || len(res.GetUnobserved()) != 0 {
		t.Errorf("complete until %v, unobserved %v", res.GetCompleteUntil(), res.GetUnobserved())
	}
}

// Of two gaps behind a count, the earlier is named, whatever the order
// the provider gave them in.
func TestReplayUnobservedNamesTheEarlierGap(t *testing.T) {
	f := observedFacts()
	f.Gaps = []api.ReplayGap{
		{Kind: api.GapCollector, Start: rAt(40, 0), End: rAt(45, 0), Event: ev(rAt(40, 0), "collector.gap", "")},
		{Kind: api.GapCollector, Start: rAt(20, 0), End: rAt(30, 0), Event: ev(rAt(20, 0), "collector.gap", "")},
	}
	for _, u := range api.FinishReplay(f).GetUnobserved() {
		if !u.GetFrom().AsTime().Equal(rAt(20, 0)) {
			t.Errorf("%v", u)
		}
	}
}
