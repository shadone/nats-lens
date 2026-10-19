package api

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// k8s adds the Kubernetes steps between A and B: node drains and
// upgrades folded by kind, NATS pod failures and the pod events no
// restart or stop took in, folded by kind and server.
func (b *replayBuilder) k8s() {
	var evs []*v1.Event
	for _, e := range b.f.Events {
		k := e.GetKind()
		if b.used[e.GetId()] || !b.inRange(e.GetTime().AsTime()) {
			continue
		}
		if slices.Contains(ReplayK8sStepKinds, k) || slices.Contains(ReplayPodKinds, k) {
			evs = append(evs, e)
		}
	}
	key := func(e *v1.Event) string {
		if strings.HasPrefix(e.GetKind(), "k8s.node.") {
			return e.GetKind()
		}
		return e.GetKind() + "|" + e.GetServer()
	}
	at := func(e *v1.Event) time.Time { return e.GetTime().AsTime() }
	for _, grp := range foldGroups(evs, key, at, b.window) {
		first, last := grp[0], grp[len(grp)-1]
		d := &draft{kind: v1.ReplayStepKind_REPLAY_STEP_KIND_KUBERNETES, time: at(first)}
		switch first.GetKind() {
		case "k8s.pod.failed_scheduling", "k8s.pod.backoff", "k8s.pod.oom_killed":
			// The reason Kubernetes gives, which the title leaves out.
			d.severity, d.body = v1.Severity_SEVERITY_WARNING, last.GetSummary()
		}
		d.body = joinSentences(d.body, b.windowText(at(first)))
		if len(grp) == 1 {
			d.title = k8sTitle(first)
		} else {
			d.title = k8sFoldTitle(grp, at(first), at(last))
			d.end = at(last)
		}
		for _, e := range grp {
			d.evidence = append(d.evidence, eventEvidence(e))
			if e.GetServer() != "" {
				d.entities = append(d.entities, serverEntity(e.GetServer()))
			}
		}
		b.add(d)
	}
}

// windowText names the Kubernetes window a step at t lies in; empty
// when none does. The body of a Kubernetes step never repeats its event's
// summary, which its evidence shows (proposed, not yet confirmed).
func (b *replayBuilder) windowText(t time.Time) string {
	for _, w := range b.f.Windows {
		if !t.Before(w.GetStart().AsTime()) && !t.After(w.GetEnd().AsTime()) {
			return fmt.Sprintf("Within the %s window, %s.", cmp.Or(w.GetLabel(), "Kubernetes"), spanText(w.GetStart().AsTime(), w.GetEnd().AsTime()))
		}
	}
	return ""
}

// electionKey identifies the asset of an election: the matching leader
// at A's entity key, else the names the event gives.
func (b *replayBuilder) electionKey(e *v1.Event, atA map[string][]ReplayLeader) (string, *ReplayLeader) {
	for i, l := range atA[e.GetStream()+"/"+e.GetConsumer()] {
		if sameAsset(e, l.Entity) {
			return l.Entity.GetKey(), &atA[e.GetStream()+"/"+e.GetConsumer()][i]
		}
	}
	return "event:" + e.GetAccount() + "/" + e.GetStream() + "/" + e.GetConsumer(), nil
}

// elections walks the stream and consumer elections between A and B in
// time order and reports, by event ID, whether each moved the lead from a
// known other server, with the entity of each.
func (b *replayBuilder) elections() (moved map[string]bool, entity map[string]*v1.EntityRef, n int) {
	atA := map[string][]ReplayLeader{}
	for _, l := range b.f.LeadersAtA {
		k := l.Entity.GetStream() + "/" + l.Entity.GetConsumer()
		atA[k] = append(atA[k], l)
	}
	leader := map[string]string{}
	moved, entity = map[string]bool{}, map[string]*v1.EntityRef{}
	for _, e := range b.f.Events {
		if (e.GetKind() != "stream.leader_elected" && e.GetKind() != "consumer.leader_elected") || !b.inRange(e.GetTime().AsTime()) {
			continue
		}
		n++
		key, l := b.electionKey(e, atA)
		prev, known := leader[key]
		if !known && l != nil {
			prev, known = l.Server, true
		}
		moved[e.GetId()] = known && prev != "" && prev != e.GetServer()
		leader[key] = e.GetServer()
		entity[e.GetId()] = eventEntity(e)
		if l != nil {
			entity[e.GetId()] = l.Entity
		}
	}
	return moved, entity, n
}

// metaElections reports, by event ID, the meta leader before each meta
// election between A and B ("" when unknown).
func (b *replayBuilder) metaElections() map[string]string {
	prev, out := b.f.MetaLeaderAtA, map[string]string{}
	for _, e := range b.f.Events {
		if e.GetKind() != "meta.leader_elected" || !b.inRange(e.GetTime().AsTime()) {
			continue
		}
		out[e.GetId()] = prev
		prev = e.GetServer()
	}
	return out
}

// leaders adds the meta leader steps and the stream and consumer
// elections no restart took in, folded by the new leader.
func (b *replayBuilder) leaders() {
	moved, entity, _ := b.elections()
	var evs []*v1.Event
	for _, e := range b.f.Events {
		if (e.GetKind() == "stream.leader_elected" || e.GetKind() == "consumer.leader_elected") && !b.used[e.GetId()] &&
			b.inRange(e.GetTime().AsTime()) {
			evs = append(evs, e)
		}
	}
	at := func(e *v1.Event) time.Time { return e.GetTime().AsTime() }
	for _, grp := range foldGroups(evs, (*v1.Event).GetServer, at, b.window) {
		first, last := grp[0], grp[len(grp)-1]
		d := &draft{kind: v1.ReplayStepKind_REPLAY_STEP_KIND_LEADERS, time: at(first)}
		var labels []string
		n := 0
		for _, e := range grp {
			d.evidence = append(d.evidence, eventEvidence(e))
			if ent := entity[e.GetId()]; ent != nil {
				d.entities = append(d.entities, ent)
				labels = append(labels, entityLabel(ent))
			}
			if moved[e.GetId()] {
				n++
			}
		}
		if len(grp) == 1 {
			d.title = fmt.Sprintf("Leader of %s elected on %s", cmpOr(labels, first.GetStream()), first.GetServer())
		} else {
			d.title = fmt.Sprintf("%s elected on %s", Plural(len(grp), "leader"), first.GetServer())
			d.end = at(last)
		}
		var moves string
		switch {
		case len(grp) == 1 && n == 1:
			moves = "The lead moved from another server."
		case n > 0:
			moves = fmt.Sprintf("%d of them moved the lead from another server.", n)
		}
		if len(grp) > 1 {
			d.body = joinSentences(listText(labels)+".", moves)
		} else {
			d.body = moves
		}
		b.add(d)
	}
	prev := b.metaElections()
	var metas []*v1.Event
	for _, e := range b.f.Events {
		if _, ok := prev[e.GetId()]; ok && e.GetKind() == "meta.leader_elected" {
			metas = append(metas, e)
		}
	}
	for _, grp := range foldGroups(metas, func(*v1.Event) string { return "meta" }, at, b.window) {
		first, last := grp[0], grp[len(grp)-1]
		p := prev[first.GetId()]
		if len(grp) == 1 && p == first.GetServer() {
			// An election that keeps the leader changes nothing
			// (proposed, not yet confirmed).
			continue
		}
		d := &draft{kind: v1.ReplayStepKind_REPLAY_STEP_KIND_LEADERS, time: at(first), entities: []*v1.EntityRef{serverEntity(last.GetServer())}}
		for _, e := range grp {
			d.evidence = append(d.evidence, eventEvidence(e))
		}
		switch {
		case len(grp) > 1:
			d.title = fmt.Sprintf("Meta leader elected %d times, last on %s", len(grp), last.GetServer())
			d.end = at(last)
		case p != "" && p != first.GetServer():
			d.title = fmt.Sprintf("Meta leader moves from %s to %s", p, first.GetServer())
		default:
			d.title = "Meta leader elected on " + first.GetServer()
		}
		b.add(d)
	}
}

func cmpOr(labels []string, def string) string {
	if len(labels) > 0 {
		return labels[0]
	}
	return def
}

// gaps adds a step per gap that overlaps A to B. A time without
// collections inside a time nats-lens was not running is told by the
// latter, with the former's collections as evidence.
func (b *replayBuilder) gaps() {
	var collector []*draft
	var plain []ReplayGap
	for _, g := range b.f.Gaps {
		if g.Start.After(b.f.B) || (!g.End.IsZero() && !g.End.After(b.f.A)) {
			continue
		}
		if g.Kind == GapCollections {
			plain = append(plain, g)
			continue
		}
		d := b.gapStep(g)
		if g.Kind == GapCollector {
			collector = append(collector, d)
		}
		b.add(d)
	}
	for _, g := range plain {
		ev := []*v1.ReplayEvidence{collectionEvidence(g.BeforeEpoch, g.BeforeAt, nil), collectionEvidence(g.AfterEpoch, g.AfterAt, nil)}
		if i := slices.IndexFunc(collector, func(d *draft) bool { return overlapsSpan(d, g) }); i >= 0 {
			collector[i].evidence = append(collector[i].evidence, ev...)
			continue
		}
		d := b.gapStep(g)
		d.evidence = append(d.evidence, ev...)
		b.add(d)
	}
}

// overlapsSpan reports whether a gap overlaps a step's span; an open
// step reaches to any later time.
func overlapsSpan(d *draft, g ReplayGap) bool {
	end := d.end
	return (g.End.IsZero() || !g.End.Before(d.time)) && (d.openAtB || end.IsZero() || !g.Start.After(end))
}

func (b *replayBuilder) gapStep(g ReplayGap) *draft {
	d := &draft{kind: v1.ReplayStepKind_REPLAY_STEP_KIND_GAP, time: g.Start}
	open := g.End.IsZero() || g.End.After(b.f.B)
	if open {
		d.openAtB = true
	} else {
		d.end = g.End
	}
	var what, since string
	switch g.Kind {
	case GapCollector:
		what, since = "nats-lens was not running", "nats-lens not running"
	case GapKubernetes:
		what, since = "Kubernetes was not watched", "Kubernetes not watched"
	default:
		what, since = "No collections", "No collections"
	}
	// A gap open at B is titled up to B: the step names no time after it.
	switch {
	case open:
		d.title = since + " since " + clockText(g.Start) + ", still at B"
	default:
		d.title = fmt.Sprintf("%s from %s to %s", what, clockText(g.Start), clockText(g.End))
	}
	if g.Event != nil {
		d.evidence = append(d.evidence, eventEvidence(g.Event))
		d.body = g.Event.GetSummary()
	}
	if g.Kind == GapCollections && !open {
		d.body = fmt.Sprintf("No collection for %s; nothing is known of that time.", durText(g.End.Sub(g.Start)))
	}
	if g.Kind == GapCollections && open {
		d.body = "No collection after " + clockText(g.Start) + " up to B; nothing is known of that time."
	}
	return d
}
