package fake

import (
	"cmp"
	"slices"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// replayCatchUps are the runs of replicas behind their leaders that the
// scenario's restarts between A and B start: with scenario.catchUp set,
// every replica a restarted server follows is behind from its new
// process's first collection. By size, the smallest first, stream i of
// n catches up after catchUp × (i+1)/n, its durables in half that; a
// replica not current by B's collection is still behind there. Its peak
// lag is a hundredth of the stream's messages (a twentieth of that for a
// durable) and falls evenly while it catches up.
func (s scen) replayCatchUps(f *api.ReplayFacts) []api.ReplayCatchUp {
	if s.sc.catchUp == 0 || f.EpochB == 0 {
		return nil
	}
	var out []api.ReplayCatchUp
	for _, r := range f.Restarts {
		var streams []streamSpec
		for _, sp := range s.sc.streams {
			if len(s.sc.peers(sp)) > 1 && s.holds(r.Server, sp, r.FirstSeen) && s.streamLeaderAt(sp, r.FirstSeen) != r.Server {
				streams = append(streams, sp)
			}
		}
		slices.SortStableFunc(streams, func(x, y streamSpec) int { return cmp.Compare(x.msgs, y.msgs) })
		for i, sp := range streams {
			d := s.sc.catchUp * time.Duration(i+1) / time.Duration(len(streams))
			peak := max(sp.msgs/100, 200)
			out = append(out, s.catchUpRun(f, r, streamRef(sp.name), d, peak)...)
			for _, c := range s.consumersAt(sp, r.FirstSeen) {
				if s.consumerLeaderAt(sp, c, r.FirstSeen) != r.Server {
					out = append(out, s.catchUpRun(f, r, consumerRef(sp.name, c), d/2, max(peak/20, 20))...)
				}
			}
		}
	}
	return out
}

// catchUpRun is one replica's run from the restart's first collection,
// current again after d rounded up to a collection, or still behind at
// B's collection; none when B's collection precedes the restart's.
func (s scen) catchUpRun(f *api.ReplayFacts, r api.ReplayRestart, ref *v1.EntityRef, d time.Duration, peak int64) []api.ReplayCatchUp {
	n := max(int64((d+sampleEvery-1)/sampleEvery), 1) // collections behind
	c := api.ReplayCatchUp{Server: r.Server, Entity: ref, From: r.FirstSeen, FromEpoch: r.NewEpoch, PeakLag: ptr(peak)}
	done := r.FirstSeen.Add(time.Duration(n) * sampleEvery)
	switch {
	case f.EpochBAt.Before(r.FirstSeen):
		return nil
	case !done.After(f.EpochBAt):
		c.Last, c.LastEpoch, c.Behind = done.Add(-sampleEvery), r.NewEpoch+n-1, int(n)
		c.Next, c.NextEpoch, c.Current, c.CurrentEpoch = done, r.NewEpoch+n, done, r.NewEpoch+n
	default:
		k := int64(f.EpochBAt.Sub(r.FirstSeen) / sampleEvery)
		c.Last, c.LastEpoch, c.Behind = f.EpochBAt, f.EpochB, int(k+1)
	}
	c.LastLag = ptr(peak * (n - int64(c.Last.Sub(r.FirstSeen)/sampleEvery)) / n)
	return []api.ReplayCatchUp{c}
}
