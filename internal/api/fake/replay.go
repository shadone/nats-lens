package fake

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// replayRestartWindow is the demo's checks.restart_window.
const replayRestartWindow = 10 * time.Minute

// Replay builds the facts of the story between A and B from the
// scenario: restarts from the servers' start times and down periods,
// servers that stopped answering, the events and the findings'
// transitions, the windows, the gone assets of scenario.gone and the
// times without collections. The meta leader at A is left unknown, as
// the scenario names only the current one. A older than the demo's 14
// days of retention is refused.
func (p *Provider) Replay(_ context.Context, req *v1.GetReplayRequest) (*api.ReplayFacts, error) {
	s := p.scen()
	a, b := req.GetA().AsTime().UTC(), req.GetB().AsTime().UTC()
	if a.Before(s.now.Add(-14 * 24 * time.Hour)) {
		return nil, fmt.Errorf("%w: A is older than retention (14d); Replay reads the database only", api.ErrInvalid)
	}
	f := &api.ReplayFacts{A: a, B: b, Until: earliest(s.now, b.Add(api.ReplayLookahead)), Interval: sampleEvery,
		RestartWindow: replayRestartWindow, HistoryStart: s.dataStart(), Kubernetes: s.sc.k8sLanes != nil}
	f.EpochAAt, f.EpochA = s.replayEpoch(a)
	f.EpochBAt, f.EpochB = s.replayEpoch(b)
	_, _, f.Collected = s.epochBefore(&v1.TimeRange{Start: ts(a), End: ts(b.Add(time.Nanosecond))})
	all := p.events(s)
	f.EventsCaptured = len(all) > 0
	kinds := api.ReplayEventKinds()
	for _, e := range all {
		at := e.GetTime().AsTime()
		if at.Before(a.Add(-replayRestartWindow)) || at.After(f.Until) || !slices.Contains(kinds, e.GetKind()) ||
			(strings.HasPrefix(e.GetKind(), "k8s.pod.") && e.GetServer() == "") {
			continue
		}
		f.Events = append(f.Events, e)
	}
	f.Restarts, f.Stops = s.replayServers(f)
	f.Transitions = s.replayTransitions(p.findings(s), a, f.Until)
	for _, sp := range s.sc.streams {
		if l := s.leader(sp); l != "" && f.EpochA != 0 {
			f.LeadersAtA = append(f.LeadersAtA, api.ReplayLeader{Entity: streamRef(sp.name), Server: l})
		}
	}
	f.Windows = s.windows(&v1.TimeRange{Start: ts(a), End: ts(b)})
	if s.sc.gone != nil && f.EpochA != 0 && f.EpochB != 0 {
		f.Gone, f.GoneRead = s.sc.gone(s, f), true
	}
	f.Gaps = s.replayGaps(a, b)
	f.ClientReconnects = replayReconnects(all, a, b)
	f.Nodes = replayNodes(all, a, b)
	return f, nil
}

// replayEpoch is the epoch read as the state at t: the newest that
// started at or before t, within ReplayEpochReach intervals.
func (s scen) replayEpoch(t time.Time) (time.Time, int64) {
	at, n, ok := s.epochBefore(&v1.TimeRange{Start: ts(t.Add(-api.ReplayEpochReach * sampleEvery)), End: ts(t.Add(time.Nanosecond))})
	if !ok {
		return time.Time{}, 0
	}
	return at, n
}

// epochNear walks the epochs from the one at or before t by step (one
// interval back or forward) to the first in which the server answered,
// at most limit; ok is false when there is none.
func (s scen) epochNear(sp serverSpec, t time.Time, step, limit time.Duration) (time.Time, int64, bool) {
	at, n := s.replayEpoch(t)
	if n == 0 {
		return time.Time{}, 0, false
	}
	if step > 0 && at.Before(t) {
		at, n = at.Add(step), n+1
	}
	for range int(limit / sampleEvery) {
		if at.After(s.lastEpoch()) || at.Before(s.dataStart()) {
			return time.Time{}, 0, false
		}
		if s.answers(sp, at) {
			return at, n, true
		}
		at, n = at.Add(step), n+int64(step/sampleEvery)
	}
	return time.Time{}, 0, false
}

// answers reports whether a server answered in the epoch at at.
func (s scen) answers(sp serverSpec, at time.Time) bool {
	return !s.downAt(sp, at) && (sp.answering() || at.Before(sp.down.Add(s.shift)))
}

// replayServers are the restarts (a process with an earlier one that
// started after A and at or before B and was first collected by B) and
// the stops (a server that answered in A's collection or later and not
// in B's, with when it answered again up to Until), by the store's rules.
func (s scen) replayServers(f *api.ReplayFacts) ([]api.ReplayRestart, []api.ReplayStop) {
	var restarts []api.ReplayRestart
	var stops []api.ReplayStop
	for _, sp := range s.sc.servers {
		if start := s.started(sp); sp.oldID != "" && start.After(f.A) && !start.After(f.B) {
			last, oldEpoch, ok1 := s.epochNear(sp, start.Add(-time.Nanosecond), -sampleEvery, time.Hour)
			first, newEpoch, ok2 := s.epochNear(sp, start, sampleEvery, time.Hour)
			if ok1 && ok2 && !first.After(f.B) {
				restarts = append(restarts, api.ReplayRestart{Server: sp.name, OldID: sp.oldID, NewID: sp.id, LastSeen: last,
					FirstSeen: first, Started: start, OldEpoch: oldEpoch, NewEpoch: newEpoch, OldVersion: sp.version, NewVersion: sp.version})
			}
		}
		if f.EpochB == 0 || s.answers(sp, f.EpochBAt) {
			continue
		}
		from := f.EpochAAt
		if f.EpochA == 0 {
			from = f.A
		}
		last, n, ok := s.epochNear(sp, f.EpochBAt.Add(-sampleEvery), -sampleEvery, f.EpochBAt.Sub(from)+sampleEvery)
		if !ok || last.Before(from) {
			continue
		}
		stop := api.ReplayStop{Server: sp.name, LastSeen: last, LastEpoch: n}
		if back, bn, ok := s.epochNear(sp, f.B.Add(time.Nanosecond), sampleEvery, f.Until.Sub(f.B)+sampleEvery); ok && !back.After(f.Until) {
			stop.BackAt, stop.BackEpoch = back, bn
		}
		stops = append(stops, stop)
	}
	return restarts, stops
}

// replayTransitions are the findings' transitions from A to until, and
// for a finding still firing at A the newest before A and the firing that
// started it.
func (s scen) replayTransitions(fs []*v1.Finding, a, until time.Time) []api.ReplayTransition {
	var out []api.ReplayTransition
	for _, f := range fs {
		trs := s.transitions(f) // newest first
		before := false
		for _, t := range trs {
			at := t.GetTime().AsTime()
			if at.After(until) {
				continue
			}
			if at.Before(a) {
				if !before && t.GetState() == resolved {
					break
				}
				before = true
			}
			out = append(out, api.ReplayTransition{FindingID: f.GetId(), Check: f.GetCheckCode(), CheckName: f.GetCheckName(),
				Entity: f.GetEntity(), Time: at, State: t.GetState(), Severity: t.GetSeverity(), Detail: t.GetDetail()})
			if before && t.GetState() == firing {
				break
			}
		}
	}
	return out
}

// replayGaps are the times without collections that overlap A to B:
// before the history starts, and after the data ends.
func (s scen) replayGaps(a, b time.Time) []api.ReplayGap {
	reach := api.ReplayEpochReach * sampleEvery
	var out []api.ReplayGap
	if first := s.dataStart(); first.Sub(a) > reach && first.Before(b) {
		_, n, _ := s.epochBefore(&v1.TimeRange{Start: ts(first), End: ts(first.Add(sampleEvery))})
		out = append(out, api.ReplayGap{Kind: api.GapCollections, Start: a, End: first, AfterEpoch: n, AfterAt: first})
	}
	if last := s.lastEpoch(); b.Sub(last) > reach && last.After(a.Add(-reach)) {
		out = append(out, api.ReplayGap{Kind: api.GapCollections, Start: last, BeforeEpoch: s.epochNumber(last), BeforeAt: last})
	}
	return out
}

// replayReconnects pairs the disconnects not closed by the client with the
// connects of each connection name between A and B, as client-004 does
// with server shutdowns counted; routine events are left out.
func replayReconnects(evs []*v1.Event, a, b time.Time) int32 {
	drops, connects := map[string]int{}, map[string]int{}
	for _, e := range evs {
		if at := e.GetTime().AsTime(); at.Before(a) || at.After(b) || routineEvent(e) {
			continue
		}
		switch e.GetKind() {
		case "client.disconnect":
			if e.GetReason() != "Client Closed" {
				drops[e.GetClientName()]++
			}
		case "client.connect":
			connects[e.GetClientName()]++
		case "client.churn":
			var raw struct {
				Connects int `json:"connects"`
			}
			if json.Unmarshal([]byte(e.GetRaw()), &raw) == nil {
				connects[e.GetClientName()] += raw.Connects
			}
		}
	}
	n := 0
	for name, d := range drops {
		n += min(d, connects[name])
	}
	return int32(n)
}

// replayNodes counts the nodes drained and upgraded between A and B by
// the node the payload names, as the store does: an event without one is
// not counted. It also counts the node pools of the upgraded ones.
func replayNodes(evs []*v1.Event, a, b time.Time) api.ReplayNodes {
	drained, upgraded, pools := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, e := range evs {
		if at := e.GetTime().AsTime(); at.Before(a) || at.After(b) {
			continue
		}
		var raw struct {
			Node string `json:"node"`
			Pool string `json:"pool"`
		}
		_ = json.Unmarshal([]byte(e.GetRaw()), &raw)
		node := raw.Node
		if node == "" {
			continue
		}
		switch e.GetKind() {
		case "k8s.node.drained":
			drained[node] = true
		case "k8s.node.upgrade":
			upgraded[node] = true
			if raw.Pool != "" {
				pools[raw.Pool] = true
			}
		}
	}
	return api.ReplayNodes{Upgraded: int32(len(upgraded)), Drained: int32(len(drained)), Pools: int32(len(pools)), PoolLabels: len(pools) > 0}
}
