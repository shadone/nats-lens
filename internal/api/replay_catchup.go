package api

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// catchUpChecks are the checks whose transitions a catch-up step takes
// in: stream-003 about one of its streams, server-012 about its server.
var catchUpChecks = map[string]bool{"stream-003": true, "server-012": true}

// How a replica's run behind its leader ended, as far as the collections
// tell.
type catchUpEnd int

const (
	// caughtUp: reported current in the collection right after the last
	// that reported it behind, with no time without collections between.
	caughtUp catchUpEnd = iota
	// currentUnseen: reported current later, but a time without
	// collections or without the leader's report hides when.
	currentUnseen
	// behindAtB: reported behind in the collection at B.
	behindAtB
	// unknownAtB: reported behind earlier, and neither behind nor current
	// after that up to B.
	unknownAtB
)

// catchUpAsset is one replica on a server: its runs between A and B
// joined, with how the last ended.
type catchUpAsset struct {
	entity    *v1.EntityRef
	from      time.Time
	fromEpoch int64
	run       ReplayCatchUp // the last run
	peak      *int64
	end       catchUpEnd
	// why says what hides the end of currentUnseen and unknownAtB.
	why string
}

// catchUpSummary is how the catch-up of a restarted server ended, for
// its restart step's body.
type catchUpSummary struct {
	from, end time.Time
	behind    bool // a replica still behind at B
	allSeen   bool // every replica seen current again
}

// catchUpEndOf classifies a run's end, with what hides it when hidden.
func (b *replayBuilder) catchUpEndOf(c ReplayCatchUp) (catchUpEnd, string) {
	reach := ReplayEpochReach * b.f.Interval
	switch {
	case c.CurrentEpoch != 0 && c.NextEpoch == c.CurrentEpoch && c.Current.Sub(c.Last) <= reach:
		return caughtUp, ""
	case c.CurrentEpoch != 0 && c.NextEpoch != c.CurrentEpoch:
		return currentUnseen, fmt.Sprintf("the leader did not report the replica from %s to %s", clockText(c.Next), clockText(c.Current))
	case c.CurrentEpoch != 0:
		return currentUnseen, fmt.Sprintf("no collection from %s to %s", clockText(c.Last), clockText(c.Current))
	case b.f.EpochB != 0 && c.LastEpoch == b.f.EpochB:
		return behindAtB, ""
	case c.NextEpoch != 0:
		return unknownAtB, "the leader did not report the replica after " + clockText(c.Last)
	}
	return unknownAtB, "no collection after " + clockText(c.Last) + " up to B"
}

// restartedInto reports whether a restart's new process of the server
// was first collected in the epoch.
func (b *replayBuilder) restartedInto(server string, epoch int64) bool {
	return slices.ContainsFunc(b.f.Restarts, func(r ReplayRestart) bool { return r.Server == server && r.NewEpoch == epoch })
}

// catchUps adds one step per server whose replicas a leader reported
// behind between A and B. A run of one collection is left out as a burst
// of writes, unless it is the first collection of the server's restarted
// process. It returns each server's summary.
func (b *replayBuilder) catchUps() map[string]catchUpSummary {
	byServer := map[string]map[string]*catchUpAsset{}
	runs := slices.Clone(b.f.CatchUps)
	slices.SortStableFunc(runs, func(x, y ReplayCatchUp) int { return x.From.Compare(y.From) })
	for _, c := range runs {
		if c.Entity == nil || (c.Behind < 2 && !b.restartedInto(c.Server, c.FromEpoch)) {
			continue
		}
		if byServer[c.Server] == nil {
			byServer[c.Server] = map[string]*catchUpAsset{}
		}
		key := c.Entity.GetType().String() + "|" + c.Entity.GetKey()
		a := byServer[c.Server][key]
		if a == nil {
			a = &catchUpAsset{entity: c.Entity, from: c.From, fromEpoch: c.FromEpoch}
			byServer[c.Server][key] = a
		}
		a.run = c
		if c.PeakLag != nil && (a.peak == nil || *c.PeakLag > *a.peak) {
			a.peak = c.PeakLag
		}
		a.end, a.why = b.catchUpEndOf(c)
	}
	out := map[string]catchUpSummary{}
	for server, m := range byServer {
		assets := slices.Collect(func(yield func(*catchUpAsset) bool) {
			for _, a := range m {
				if !yield(a) {
					return
				}
			}
		})
		slices.SortFunc(assets, func(x, y *catchUpAsset) int {
			return cmp.Or(x.from.Compare(y.from), cmp.Compare(x.entity.GetType(), y.entity.GetType()), cmp.Compare(x.entity.GetKey(), y.entity.GetKey()))
		})
		d := b.catchUpStep(server, assets)
		b.add(d)
		out[server] = catchUpSummary{from: d.time, end: d.end, behind: slices.ContainsFunc(assets, func(a *catchUpAsset) bool { return a.end == behindAtB }),
			allSeen: !slices.ContainsFunc(assets, func(a *catchUpAsset) bool { return a.end != caughtUp })}
	}
	return out
}

// catchUpStep words one server's step.
func (b *replayBuilder) catchUpStep(server string, assets []*catchUpAsset) *draft {
	d := &draft{kind: v1.ReplayStepKind_REPLAY_STEP_KIND_CATCH_UP, time: assets[0].from, entities: []*v1.EntityRef{serverEntity(server)}}
	by := map[catchUpEnd][]*catchUpAsset{}
	var labels []string
	atA := false
	for _, a := range assets {
		by[a.end] = append(by[a.end], a)
		d.entities = append(d.entities, a.entity)
		labels = append(labels, entityLabel(a.entity))
		atA = atA || (b.f.EpochA != 0 && a.fromEpoch == b.f.EpochA)
	}
	caught, unseen, behind, unknown := by[caughtUp], by[currentUnseen], by[behindAtB], by[unknownAtB]
	var end time.Time
	var endEpoch int64
	for _, a := range append(slices.Clone(caught), unseen...) {
		if a.run.Current.After(end) {
			end, endEpoch = a.run.Current, a.run.CurrentEpoch
		}
	}
	if len(behind)+len(unknown) > 0 {
		d.openAtB = true
	} else {
		d.end = end
	}
	switch {
	case len(behind) > 0:
		d.severity = v1.Severity_SEVERITY_WARNING
		d.title = server + " still catching up at B: " + behindText(behind)
	case len(caught) == len(assets):
		d.title = server + " catches up " + assetsText(assets)
	default:
		d.title = server + " falls behind on " + assetsText(assets)
	}
	var body []string
	body = append(body, "Replicas of "+listText(labels)+".")
	if atA {
		body = append(body, "Behind already in the collection at A.")
	}
	if len(caught) > 0 {
		var by time.Time
		for _, a := range caught {
			by = latest(by, a.run.Current)
		}
		lead := "Current again by "
		if len(caught) < len(assets) {
			lead = capitalize(assetsText(caught)) + " current again by "
		}
		body = append(body, lead+clockText(by)+".", slowestText(caught, len(caught) == len(assets)))
	}
	if len(unseen) > 0 {
		var by time.Time
		for _, a := range unseen {
			by = latest(by, a.run.Current)
		}
		they, verb := "they", "were"
		if len(unseen) == 1 {
			they, verb = "it", "was"
		}
		body = append(body, fmt.Sprintf("%s %s current by %s, but when %s caught up was not observed: %s.", capitalize(assetsText(unseen)), verb,
			clockText(by), they, unseen[0].why))
	}
	if len(unknown) > 0 {
		body = append(body, fmt.Sprintf("%s not observed at B: %s.", capitalize(assetsText(unknown)), unknown[0].why))
	}
	if len(behind) > 0 && len(behind) < len(assets) {
		var names []string
		for _, a := range behind {
			names = append(names, entityLabel(a.entity))
		}
		body = append(body, "Behind at B: "+listText(names)+".")
	}
	d.body = joinSentences(body...)
	// Placement at the start and at the end (B's collection while open),
	// the slowest replica's asset at its end, and the findings.
	first := assets[0]
	d.evidence = append(d.evidence, catchUpEvidence(first.fromEpoch, first.from, nil, "in Placement"))
	if d.openAtB {
		d.evidence = append(d.evidence, catchUpEvidence(b.f.EpochB, b.f.EpochBAt, nil, "in Placement"))
	} else {
		d.evidence = append(d.evidence, catchUpEvidence(endEpoch, end, nil, "in Placement"))
	}
	if s := slowest(append(slices.Clone(caught), unseen...)); s != nil && !d.openAtB {
		d.evidence = append(d.evidence, catchUpEvidence(s.run.CurrentEpoch, s.run.Current, s.entity, "for "+entityLabel(s.entity)))
	}
	if len(behind) > 0 {
		s := largest(behind)
		d.evidence = append(d.evidence, catchUpEvidence(s.run.LastEpoch, s.run.Last, s.entity, "for "+entityLabel(s.entity)))
	}
	to := b.f.B
	if !d.openAtB {
		to = b.upToB(d.end.Add(b.f.Interval))
	}
	keys := map[string]bool{}
	for _, a := range assets {
		keys[a.entity.GetKey()] = true
	}
	for _, id := range b.findingIDs() {
		for _, t := range b.byFinding[id] {
			if !catchUpChecks[t.Check] || t.Time.Before(d.time) || t.Time.After(to) {
				continue
			}
			if (t.Check == "server-012" && t.Entity.GetKey() == server) || (t.Check == "stream-003" && keys[t.Entity.GetKey()]) {
				b.takenIn[transitionKey(t)] = true
				d.evidence = append(d.evidence, findingEvidence(t))
				if t.State != v1.FindingState_FINDING_STATE_RESOLVED {
					d.severity = worse(d.severity, t.Severity)
				}
			}
		}
	}
	return d
}

// catchUpEvidence is a collection named for what it opens: "collection
// of 09:02:00 in Placement".
func catchUpEvidence(epoch int64, at time.Time, e *v1.EntityRef, what string) *v1.ReplayEvidence {
	ev := collectionEvidence(epoch, at, e)
	if ev != nil {
		ev.Label += " " + what
	}
	return ev
}

// assetsText counts assets by kind: "14 streams and 3 consumers".
func assetsText(as []*catchUpAsset) string {
	streams, consumers := 0, 0
	for _, a := range as {
		if a.entity.GetType() == v1.EntityType_ENTITY_TYPE_CONSUMER {
			consumers++
		} else {
			streams++
		}
	}
	switch {
	case consumers == 0:
		return Plural(streams, "stream")
	case streams == 0:
		return Plural(consumers, "consumer")
	}
	return Plural(streams, "stream") + " and " + Plural(consumers, "consumer")
}

// behindText says what was still behind at B and by how much: "3 streams
// behind, the largest 18,400 operations behind", or for one "orders
// 18,400 operations behind".
func behindText(as []*catchUpAsset) string {
	l := largest(as)
	if len(as) == 1 {
		if l.run.LastLag == nil {
			return entityLabel(l.entity) + " behind"
		}
		return entityLabel(l.entity) + " " + digits(*l.run.LastLag) + " operations behind"
	}
	out := assetsText(as) + " behind"
	if l.run.LastLag != nil {
		out += ", the largest " + digits(*l.run.LastLag) + " operations behind"
	}
	return out
}

// largest is the asset with the largest lag in its last run's last
// collection, the first on a tie or without lags.
func largest(as []*catchUpAsset) *catchUpAsset {
	out := as[0]
	for _, a := range as[1:] {
		if a.run.LastLag != nil && (out.run.LastLag == nil || *a.run.LastLag > *out.run.LastLag) {
			out = a
		}
	}
	return out
}

// slowest is the asset reported current last, the larger peak lag on a
// tie; nil without any.
func slowest(as []*catchUpAsset) *catchUpAsset {
	var out *catchUpAsset
	for _, a := range as {
		switch {
		case out == nil, a.run.Current.After(out.run.Current):
			out = a
		case a.run.Current.Equal(out.run.Current) && a.peak != nil && (out.peak == nil || *a.peak > *out.peak):
			out = a
		}
	}
	return out
}

// slowestText names the slowest replica seen current again, how long it
// was behind and its peak lag: "The slowest, orders, took 7 min and was
// at most 18,400 operations behind." For the only replica it reads "It
// took …".
func slowestText(caught []*catchUpAsset, all bool) string {
	s := slowest(caught)
	took := durText(s.run.Current.Sub(s.from))
	lag := ""
	if s.peak != nil {
		lag = " and was at most " + digits(*s.peak) + " operations behind"
	}
	if len(caught) == 1 && all {
		return "It took " + took + lag + "."
	}
	return "The slowest, " + entityLabel(s.entity) + ", took " + took + lag + "."
}

// digits writes a count with thousands separators: 18,400.
func digits(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var out []byte
	for i := range len(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, s[i])
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}

// catchUpRestartText ties a restart to its server's catch-up step:
// "Its replicas were current again by 09:09:00."; empty when the server
// had none that began after its old process last answered.
func (b *replayBuilder) catchUpRestartText(r ReplayRestart) string {
	c, ok := b.catchUp[r.Server]
	if !ok || c.from.Before(r.LastSeen) {
		return ""
	}
	switch {
	case c.behind:
		return "Its replicas were still catching up at B."
	case c.allSeen:
		return "Its replicas were current again by " + clockText(c.end) + "."
	}
	return "When its replicas were current again was not observed."
}
