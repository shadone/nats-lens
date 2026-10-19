package fake

import (
	"slices"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// streamLeaderAt is a stream's leader at the scenario time at; "" for a
// leaderless stream.
func (s scen) streamLeaderAt(sp streamSpec, at time.Time) string {
	if sp.leaderless {
		return ""
	}
	return s.leaderAt(streamRef(sp.name).GetKey(), s.leader(sp), at)
}

// consumerLeaderAt is a durable's leader at the scenario time at: its
// stream's unless the scenario says otherwise.
func (s scen) consumerLeaderAt(sp streamSpec, name string, at time.Time) string {
	return s.leaderAt(consumerRef(sp.name, name).GetKey(), s.streamLeaderAt(sp, at), at)
}

// leadersAt are the leaders of the streams and durables at the scenario
// time at.
func (s scen) leadersAt(at time.Time) []api.ReplayLeader {
	var out []api.ReplayLeader
	for _, sp := range s.sc.streams {
		if l := s.streamLeaderAt(sp, at); l != "" {
			out = append(out, api.ReplayLeader{Entity: streamRef(sp.name), Server: l})
		}
		for _, n := range s.consumersAt(sp, at) {
			if l := s.consumerLeaderAt(sp, n, at); l != "" {
				out = append(out, api.ReplayLeader{Entity: consumerRef(sp.name, n), Server: l})
			}
		}
	}
	return out
}

// ledBy are the assets a server led at the scenario time at.
func (s scen) ledBy(server string, at time.Time) []*v1.EntityRef {
	var out []*v1.EntityRef
	for _, l := range s.leadersAt(at) {
		if l.Server == server {
			out = append(out, l.Entity)
		}
	}
	return out
}

// holds reports whether a server held a stream's replica at the scenario
// time at: it is a peer, and not the follower the stream names missing
// once that server's present process started.
func (s scen) holds(server string, sp streamSpec, at time.Time) bool {
	if !slices.Contains(s.sc.peers(sp), server) {
		return false
	}
	if sv, ok := s.sc.server(server); ok && server == sp.missing && !at.Before(s.started(sv)) {
		return false
	}
	return true
}

// heldBy counts the streams and durables a server held at the scenario
// time at. Ephemeral consumers are left out: the demo does not say which
// server holds them.
func (s scen) heldBy(server string, at time.Time) (streams, consumers *int32) {
	var st, co int32
	for _, sp := range s.sc.streams {
		if s.holds(server, sp, at) {
			st++
			co += int32(len(s.consumersAt(sp, at)))
		}
	}
	return &st, &co
}

// vanishedGone are the vanished durables held at A's collection and by
// no server at B's, with their delete events and findings.
func (s scen) vanishedGone(f *api.ReplayFacts, evs []*v1.Event, fs []*v1.Finding) []api.ReplayGone {
	var out []api.ReplayGone
	for _, sp := range s.sc.streams {
		for _, v := range sp.vanished {
			at := v.at.Add(s.shift)
			if !f.EpochAAt.Before(at) || f.EpochBAt.Before(at) {
				continue
			}
			g := api.ReplayGone{Entity: consumerRef(sp.name, v.name)}
			for _, name := range s.sc.peers(sp) {
				if sv, ok := s.sc.server(name); ok && s.answers(sv, f.EpochAAt) {
					g.ServersAtA = append(g.ServersAtA, name)
					if !s.answers(sv, f.EpochBAt) {
						g.NotAnsweringAtB = append(g.NotAnsweringAtB, name)
					}
				}
			}
			slices.Sort(g.ServersAtA)
			g.LastReported, g.LastEpoch = s.lastReport(sp, at)
			g.Delete = deleteEvent(evs, g.Entity, g.LastReported, f.B)
			g.FindingIDs = goneFindings(fs, g.Entity)
			out = append(out, g)
		}
	}
	return out
}

// lastReport is the last collection before the scenario time at in which
// a peer of the stream answered.
func (s scen) lastReport(sp streamSpec, at time.Time) (time.Time, int64) {
	t, n := s.replayEpoch(at.Add(-time.Nanosecond))
	for range int(time.Hour / sampleEvery) {
		if n == 0 {
			break
		}
		for _, name := range s.sc.peers(sp) {
			if sv, ok := s.sc.server(name); ok && s.answers(sv, t) {
				return t, n
			}
		}
		t, n = t.Add(-sampleEvery), n-1
	}
	return time.Time{}, 0
}

// deleteEvent is the newest delete event of a consumer or its stream
// between 30 seconds before its last report and B, as consumer-013 looks
// for one.
func deleteEvent(evs []*v1.Event, e *v1.EntityRef, last, b time.Time) *v1.Event {
	var out *v1.Event
	for _, ev := range evs {
		at := ev.GetTime().AsTime()
		if at.Before(last.Add(-30*time.Second)) || at.After(b) || ev.GetStream() != e.GetStream() {
			continue
		}
		if (ev.GetKind() == "consumer.deleted" && ev.GetConsumer() == e.GetConsumer()) || ev.GetKind() == "stream.deleted" {
			if out == nil || at.After(out.GetTime().AsTime()) {
				out = ev
			}
		}
	}
	return out
}

// goneFindings are the IDs of the findings about a gone asset.
func goneFindings(fs []*v1.Finding, e *v1.EntityRef) []string {
	var out []string
	for _, f := range fs {
		if slices.Contains(api.ReplayGoneChecks, f.GetCheckCode()) && f.GetEntity().GetKey() == e.GetKey() {
			out = append(out, f.GetId())
		}
	}
	return out
}

// globalGone is KV_label-templates: its one replica was on nats-us-4,
// which stopped answering, so no server reports it at a B after that.
func globalGone(s scen, f *api.ReplayFacts) []api.ReplayGone {
	us4, _ := s.sc.server("nats-us-4")
	if !s.answers(us4, f.EpochAAt) || s.answers(us4, f.EpochBAt) {
		return nil
	}
	last, n, _ := s.epochNear(us4, f.EpochBAt.Add(-sampleEvery), -sampleEvery, f.EpochBAt.Sub(f.EpochAAt))
	return []api.ReplayGone{{Entity: streamRef("KV_label-templates"), ServersAtA: []string{"nats-us-4"}, NotAnsweringAtB: []string{"nats-us-4"},
		LastReported: last, LastEpoch: n, FindingIDs: []string{"stream-011:" + Account + "/KV_label-templates"}}}
}
