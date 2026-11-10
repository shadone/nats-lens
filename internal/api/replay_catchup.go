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

// CatchUpMinLag is the smallest peak lag, in raft operations, of a run
// behind that makes a catch-up step, unless the run starts with the
// server's restarted process: a busy stream's followers trail by a few
// operations without catching up on anything. A run without a reported
// lag is kept.
const CatchUpMinLag = 100

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
	// collections or without a report of the replica hides when.
	currentUnseen
	// behindAtB: reported behind in the collection at B.
	behindAtB
	// unknownAtB: reported behind earlier, and neither behind nor current
	// after that up to B.
	unknownAtB
)

// catchUpAsset is one replica on a server within an episode: its runs
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

// catchUpSummary is how one catch-up episode of a server ended, for the
// restart steps it overlaps: from its start to end (B while a replica is
// still behind there).
type catchUpSummary struct {
	from, end time.Time
	behind    bool // a replica still behind at B
	allSeen   bool // every replica seen current again
}

// unreportedText says that a replica was not reported from the
// collection at next on: in that one collection when single.
func unreportedText(next time.Time, single bool) string {
	if single {
		return "the replica was not reported behind or current in the collection of " + clockText(next)
	}
	return "the replica was not reported behind or current from " + clockText(next)
}

// catchUpEndOf classifies a run's end, with what hides it when hidden. A
// collection without a report of the replica as behind or current is one
// in which the leader did not list it so, or the replica's server did not
// report the asset.
func (b *replayBuilder) catchUpEndOf(c ReplayCatchUp) (catchUpEnd, string) {
	reach := ReplayEpochReach * b.f.Interval
	switch {
	case c.CurrentEpoch != 0 && c.NextEpoch == c.CurrentEpoch && c.Current.Sub(c.Last) <= reach:
		return caughtUp, ""
	case c.CurrentEpoch != 0 && c.NextEpoch != c.CurrentEpoch:
		return currentUnseen, unreportedText(c.Next, c.CurrentEpoch-c.NextEpoch == 1)
	case c.CurrentEpoch != 0:
		return currentUnseen, fmt.Sprintf("no collection from %s to %s", clockText(c.Last), clockText(c.Current))
	case b.f.EpochB != 0 && c.LastEpoch == b.f.EpochB:
		return behindAtB, ""
	case c.NextEpoch != 0:
		return unknownAtB, unreportedText(c.Next, c.NextEpoch == b.f.EpochB)
	}
	return unknownAtB, "no collection after " + clockText(c.Last) + " up to B"
}

// startsRestart reports whether a run starts with a restarted process of
// its server: within ReplayEpochReach collect intervals of the new
// process's first collection, as its leader may first report the replica
// offline.
func (b *replayBuilder) startsRestart(c ReplayCatchUp) bool {
	reach := ReplayEpochReach * b.f.Interval
	return slices.ContainsFunc(b.f.Restarts, func(r ReplayRestart) bool {
		return r.Server == c.Server && !c.From.Before(r.FirstSeen) && !c.From.After(r.FirstSeen.Add(reach))
	})
}

// keepCatchUp reports whether a run makes part of a step: one that starts
// with a restarted process, or one behind in two collections or more with
// a peak lag of at least CatchUpMinLag (or none reported). A run of one
// collection otherwise is a burst of writes.
func (b *replayBuilder) keepCatchUp(c ReplayCatchUp) bool {
	if c.Entity == nil {
		return false
	}
	return b.startsRestart(c) || (c.Behind >= 2 && (c.PeakLag == nil || *c.PeakLag >= CatchUpMinLag))
}

// runEnd is when a run stops counting as behind: its current collection,
// B for one behind at B, else its last collection behind.
func (b *replayBuilder) runEnd(c ReplayCatchUp) time.Time {
	switch {
	case c.CurrentEpoch != 0:
		return c.Current
	case b.f.EpochB != 0 && c.LastEpoch == b.f.EpochB:
		return b.f.B
	}
	return c.Last
}

// episodes splits a server's runs, in time order, into episodes: a run
// that starts after every earlier run of the episode ended, or after a
// restart of the server that followed the episode's start, begins a new
// one.
func (b *replayBuilder) episodes(runs []ReplayCatchUp) [][]ReplayCatchUp {
	var out [][]ReplayCatchUp
	var start, end time.Time
	for _, c := range runs {
		restarted := slices.ContainsFunc(b.f.Restarts, func(r ReplayRestart) bool {
			return r.Server == c.Server && r.LastSeen.After(start) && !r.LastSeen.After(c.From)
		})
		if len(out) == 0 || c.From.After(end) || restarted {
			out = append(out, nil)
			start, end = c.From, time.Time{}
		}
		out[len(out)-1] = append(out[len(out)-1], c)
		end = latest(end, b.runEnd(c))
	}
	return out
}

// catchUps adds one step per catch-up episode of a server whose replicas
// a leader reported behind between A and B (keepCatchUp). It returns each
// server's episodes in time order.
func (b *replayBuilder) catchUps() map[string][]catchUpSummary {
	byServer := map[string][]ReplayCatchUp{}
	for _, c := range b.f.CatchUps {
		if b.keepCatchUp(c) {
			byServer[c.Server] = append(byServer[c.Server], c)
		}
	}
	out := map[string][]catchUpSummary{}
	for _, server := range slices.Sorted(mapKeys(byServer)) {
		runs := byServer[server]
		slices.SortStableFunc(runs, func(x, y ReplayCatchUp) int { return x.From.Compare(y.From) })
		for _, ep := range b.episodes(runs) {
			assets := b.catchUpAssets(ep)
			d := b.catchUpStep(server, assets)
			b.add(d)
			end := d.end
			if d.openAtB {
				end = b.f.B
			}
			out[server] = append(out[server], catchUpSummary{from: d.time, end: end,
				behind:  slices.ContainsFunc(assets, func(a *catchUpAsset) bool { return a.end == behindAtB }),
				allSeen: !slices.ContainsFunc(assets, func(a *catchUpAsset) bool { return a.end != caughtUp })})
		}
	}
	return out
}

// catchUpAssets joins an episode's runs by asset, ordered by their first
// run, then kind and key.
func (b *replayBuilder) catchUpAssets(runs []ReplayCatchUp) []*catchUpAsset {
	m := map[string]*catchUpAsset{}
	var assets []*catchUpAsset
	for _, c := range runs {
		key := c.Entity.GetType().String() + "|" + c.Entity.GetKey()
		a := m[key]
		if a == nil {
			a = &catchUpAsset{entity: c.Entity, from: c.From, fromEpoch: c.FromEpoch}
			m[key] = a
			assets = append(assets, a)
		}
		a.run = c
		if c.PeakLag != nil && (a.peak == nil || *c.PeakLag > *a.peak) {
			a.peak = c.PeakLag
		}
		a.end, a.why = b.catchUpEndOf(c)
	}
	slices.SortStableFunc(assets, func(x, y *catchUpAsset) int {
		return cmp.Or(x.from.Compare(y.from), cmp.Compare(x.entity.GetType(), y.entity.GetType()), cmp.Compare(x.entity.GetKey(), y.entity.GetKey()))
	})
	return assets
}

// whyText says what hid the end of the assets: one reason, or each
// distinct reason with the assets it hid ("no collection from 09:10:00
// to 09:12:30 (PAY); the replica was not reported … (ORDERS)").
func whyText(as []*catchUpAsset) string {
	var reasons []string
	names := map[string][]string{}
	for _, a := range as {
		if _, ok := names[a.why]; !ok {
			reasons = append(reasons, a.why)
		}
		names[a.why] = append(names[a.why], entityLabel(a.entity))
	}
	if len(reasons) == 1 {
		return reasons[0]
	}
	parts := make([]string, len(reasons))
	for i, r := range reasons {
		parts[i] = r + " (" + listText(names[r]) + ")"
	}
	return strings.Join(parts, "; ")
}

// staleLag reports whether the detail of a stream-003 transition names
// replicas ("Leader nats-0 reports nats-1 1200 operations behind, nats-2
// …") and not the server among them; a detail that names none is not.
func staleLag(detail, server string) bool {
	_, list, ok := strings.Cut(detail, " reports ")
	if !ok {
		return false
	}
	for _, item := range strings.Split(strings.TrimSuffix(list, "."), ", ") {
		if name, _, _ := strings.Cut(item, " "); name == server {
			return false
		}
	}
	return true
}

// catchUpStep words one episode of a server.
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
	// The end: the last collection that reported a replica current, or
	// for one not observed at B its last one behind.
	var end time.Time
	var endEpoch int64
	for _, a := range assets {
		at, epoch := a.run.Current, a.run.CurrentEpoch
		if a.end == unknownAtB {
			at, epoch = a.run.Last, a.run.LastEpoch
		}
		if at.After(end) {
			end, endEpoch = at, epoch
		}
	}
	if len(behind) > 0 {
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
			clockText(by), they, whyText(unseen)))
	}
	if len(unknown) > 0 {
		body = append(body, fmt.Sprintf("%s not observed at B: %s.", capitalize(assetsText(unknown)), whyText(unknown)))
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
			if (t.Check == "server-012" && t.Entity.GetKey() == server) ||
				(t.Check == "stream-003" && keys[t.Entity.GetKey()] && !staleLag(t.Detail, server)) {
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
	took := durText(s.run.Current.Sub(s.run.From))
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

// catchUpRestartText ties a restart to the catch-ups of its server that
// were under way in its window, from the old process's last answer to the
// restart window after the new one's first or the server's next restart,
// whichever comes first: a replica behind across the
// restart counts as much as one behind after it. "Its replicas were still
// catching up at B." when one is, else by the latest end: "Its replicas
// were current again by 09:09:00.", or that it was not observed; empty
// when there is none.
func (b *replayBuilder) catchUpRestartText(r ReplayRestart) string {
	// The window ends early at the server's next restart, whose own
	// catch-ups start after it.
	until := r.FirstSeen.Add(b.f.RestartWindow)
	for _, o := range b.f.Restarts {
		if o.Server == r.Server && o.LastSeen.After(r.LastSeen) && !o.LastSeen.After(until) {
			until = o.LastSeen.Add(-time.Nanosecond)
		}
	}
	var last *catchUpSummary
	behind := false
	for i, c := range b.catchUp[r.Server] {
		if c.from.After(until) || c.end.Before(r.LastSeen) {
			continue
		}
		behind = behind || c.behind
		if last == nil || c.end.After(last.end) {
			last = &b.catchUp[r.Server][i]
		}
	}
	switch {
	case last == nil:
		return ""
	case behind:
		return "Its replicas were still catching up at B."
	case last.allSeen:
		return "Its replicas were current again by " + clockText(last.end) + "."
	}
	return "When its replicas were current again was not observed."
}
