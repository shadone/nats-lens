package api

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// Gone classes.
const (
	goneDeleted = "deleted"
	goneUnknown = "unknown"
	goneSilent  = "gone"
)

func goneClass(g ReplayGone) string {
	switch {
	case g.Delete != nil:
		return goneDeleted
	case len(g.NotAnsweringAtB) > 0:
		return goneUnknown
	}
	return goneSilent
}

// gone adds the gone assets' steps, folded by stream and class, and
// returns the gone list sorted by key.
func (b *replayBuilder) gone() []*v1.GoneAsset {
	gs := slices.Clone(b.f.Gone)
	// A gone asset's findings of the gone checks, from the transitions; a
	// gone stream also takes those of its durables, which went with it.
	for i, g := range gs {
		ids := slices.Clone(g.FindingIDs)
		for _, id := range b.findingIDs() {
			t := b.byFinding[id][0]
			about := t.Entity.GetKey() == g.Entity.GetKey() ||
				(g.Entity.GetType() == v1.EntityType_ENTITY_TYPE_STREAM && t.Entity.GetType() == v1.EntityType_ENTITY_TYPE_CONSUMER &&
					t.Entity.GetAccount() == g.Entity.GetAccount() && t.Entity.GetStream() == g.Entity.GetStream())
			if slices.Contains(ReplayGoneChecks, t.Check) && about && !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
		slices.Sort(ids)
		gs[i].FindingIDs = ids
	}
	at := func(g ReplayGone) time.Time {
		if g.Delete != nil {
			return g.Delete.GetTime().AsTime()
		}
		return g.LastReported
	}
	key := func(g ReplayGone) string {
		return goneClass(g) + "|" + g.Entity.GetAccount() + "/" + g.Entity.GetStream()
	}
	for _, grp := range foldGroups(gs, key, at, b.window) {
		b.add(b.goneStep(grp, at))
	}
	out := make([]*v1.GoneAsset, 0, len(gs))
	for _, g := range gs {
		a := &v1.GoneAsset{Entity: g.Entity, ServersAtA: g.ServersAtA, NotAnsweringAtB: g.NotAnsweringAtB,
			LastReported: tsOrNil(g.LastReported), LastEpoch: g.LastEpoch, ConsumersWithIt: g.ConsumersWithIt, FindingIds: g.FindingIDs}
		if e := g.Delete; e != nil {
			a.DeleteEvent = &v1.EventRef{Id: e.GetId(), Time: e.GetTime(), Kind: e.GetKind(), Summary: e.GetSummary()}
			a.DeleteClient = clientText(e.GetClientName(), e.GetClientHost())
		}
		out = append(out, a)
	}
	slices.SortFunc(out, func(x, y *v1.GoneAsset) int { return cmp.Compare(x.GetEntity().GetKey(), y.GetEntity().GetKey()) })
	return out
}

func (b *replayBuilder) goneStep(grp []ReplayGone, at func(ReplayGone) time.Time) *draft {
	first, class := grp[0], goneClass(grp[0])
	d := &draft{kind: v1.ReplayStepKind_REPLAY_STEP_KIND_GONE, time: at(first)}
	var labels []string
	streams := 0
	for _, g := range grp {
		d.entities = append(d.entities, g.Entity)
		labels = append(labels, entityLabel(g.Entity))
		if g.Entity.GetType() == v1.EntityType_ENTITY_TYPE_STREAM {
			streams++
		}
		if g.Delete != nil {
			d.evidence = append(d.evidence, eventEvidence(g.Delete))
		}
		parent := (*v1.EntityRef)(nil)
		if g.Entity.GetType() == v1.EntityType_ENTITY_TYPE_CONSUMER {
			parent = &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_STREAM, Key: g.Entity.GetAccount() + "/" + g.Entity.GetStream(),
				Name: g.Entity.GetStream(), Account: g.Entity.GetAccount(), Stream: g.Entity.GetStream(), AccountName: g.Entity.GetAccountName()}
		}
		d.evidence = append(d.evidence, collectionEvidence(g.LastEpoch, g.LastReported, g.Entity),
			collectionEvidence(b.f.EpochB, b.f.EpochBAt, parent))
		for _, id := range g.FindingIDs {
			b.absorbed[id] = true
			for _, t := range b.byFinding[id] {
				if t.State != v1.FindingState_FINDING_STATE_RESOLVED && !t.Time.After(b.f.B) {
					d.evidence = append(d.evidence, findingEvidence(t))
					if t.Severity >= v1.Severity_SEVERITY_WARNING {
						b.hasStep[id] = true
					}
				}
			}
		}
	}
	switch class {
	case goneDeleted:
		d.severity = v1.Severity_SEVERITY_WARNING
	case goneUnknown:
		d.severity = v1.Severity_SEVERITY_WARNING
	default:
		d.severity = v1.Severity_SEVERITY_CRITICAL
	}
	what := map[string]string{goneDeleted: "deleted", goneUnknown: "not reported at B", goneSilent: "gone without a delete event"}[class]
	if len(grp) == 1 {
		d.title = assetTitle(first.Entity) + " " + what
		d.body = goneBody(first, class)
		return d
	}
	noun := "durable"
	switch streams {
	case len(grp):
		noun = "stream"
	case 0:
	default:
		noun = "asset"
	}
	d.title = fmt.Sprintf("%s on %s %s", Plural(len(grp), noun), first.Entity.GetStream(), what)
	if streams == len(grp) {
		d.title = fmt.Sprintf("%s %s", Plural(len(grp), noun), what)
	}
	d.body = joinSentences(listText(labels)+".", "Last reported at "+clockText(first.LastReported)+".")
	if class == goneDeleted {
		d.body = joinSentences(listText(labels)+".", "Deleted from "+clockText(at(first))+" to "+clockText(at(grp[len(grp)-1]))+".")
	}
	return d
}

// assetTitle names a gone asset at the start of a title: "Stream
// inventory-changes", "Durable restock-alerts on inventory-changes".
func assetTitle(e *v1.EntityRef) string {
	if e.GetType() == v1.EntityType_ENTITY_TYPE_CONSUMER {
		return "Durable " + e.GetConsumer() + " on " + e.GetStream()
	}
	return "Stream " + e.GetStream()
}

func goneBody(g ReplayGone, class string) string {
	held := "Held by " + listAll(g.ServersAtA) + " at A"
	var parts []string
	switch class {
	case goneDeleted:
		by := clientText(g.Delete.GetClientName(), g.Delete.GetClientHost())
		if by == "" {
			by = "a client nats-lens did not name"
		}
		what := "Deleted"
		if g.Delete.GetKind() == "stream.deleted" && g.Entity.GetType() == v1.EntityType_ENTITY_TYPE_CONSUMER {
			what = "Its stream was deleted"
		}
		parts = append(parts, fmt.Sprintf("%s at %s by %s.", what, clockText(g.Delete.GetTime().AsTime()), by), held+".")
	case goneUnknown:
		parts = append(parts, held+"; no server reports it at B, and "+listAll(g.NotAnsweringAtB)+" did not answer then.",
			"Last reported at "+clockText(g.LastReported)+".")
	default:
		parts = append(parts, held+", by none at B.", "Last reported at "+clockText(g.LastReported)+".")
	}
	if g.ConsumersWithIt > 0 {
		parts = append(parts, fmt.Sprintf("%s went with it.", Plural(int(g.ConsumersWithIt), "durable consumer")))
	}
	return joinSentences(parts...)
}

// listAll joins every name: "a, b and c".
func listAll(names []string) string {
	switch len(names) {
	case 0:
		return "no server"
	case 1:
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// quorumSpan is one finding's time without quorum.
type quorumSpan struct {
	t          ReplayTransition
	start, end time.Time // end zero while open
	severity   v1.Severity
	evidence   []ReplayTransition
}

// quorum adds a span step for the quorum findings that overlap A to B,
// folding the findings of one check that started within the fold window
// of each other.
func (b *replayBuilder) quorum() {
	byCheck := map[string][]*quorumSpan{}
	for _, id := range b.findingIDs() {
		trs := b.byFinding[id]
		if !quorumChecks[trs[0].Check] {
			continue
		}
		var cur *quorumSpan
		for _, t := range trs {
			if t.State == v1.FindingState_FINDING_STATE_RESOLVED {
				if cur != nil {
					cur.end = t.Time
					cur.evidence = append(cur.evidence, t)
					b.quorumSpan(byCheck, cur)
					cur = nil
				}
				continue
			}
			if cur == nil {
				cur = &quorumSpan{t: t, start: t.Time}
			}
			cur.severity = worse(cur.severity, t.Severity)
			cur.evidence = append(cur.evidence, t)
		}
		if cur != nil {
			b.quorumSpan(byCheck, cur)
		}
	}
	for _, check := range slices.Sorted(mapKeys(byCheck)) {
		spans := byCheck[check]
		slices.SortStableFunc(spans, func(x, y *quorumSpan) int { return cmp.Compare(x.t.FindingID, y.t.FindingID) })
		key := func(*quorumSpan) string { return check }
		at := func(s *quorumSpan) time.Time { return s.start }
		for _, grp := range foldGroups(spans, key, at, b.window) {
			b.quorumStep(grp)
		}
	}
}

func mapKeys[K comparable, V any](m map[K]V) func(func(K) bool) {
	return func(yield func(K) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

// quorumSpan keeps a span that overlaps A to B.
func (b *replayBuilder) quorumSpan(byCheck map[string][]*quorumSpan, s *quorumSpan) {
	if s.start.After(b.f.B) || (!s.end.IsZero() && !s.end.After(b.f.A)) || s.severity < v1.Severity_SEVERITY_WARNING {
		return
	}
	byCheck[s.t.Check] = append(byCheck[s.t.Check], s)
}

func (b *replayBuilder) quorumStep(grp []*quorumSpan) {
	first := grp[0]
	start, end, open := first.start, time.Time{}, false
	d := &draft{kind: v1.ReplayStepKind_REPLAY_STEP_KIND_QUORUM}
	var labels []string
	for _, s := range grp {
		b.hasStep[s.t.FindingID] = true
		d.severity = worse(d.severity, s.severity)
		d.entities = append(d.entities, s.t.Entity)
		labels = append(labels, entityLabel(s.t.Entity))
		if s.end.IsZero() || s.end.After(b.f.B) {
			open = true
		} else {
			end = latest(end, s.end)
		}
		stop := b.f.B
		if !s.end.IsZero() && s.end.Before(stop) {
			stop = s.end
		}
		for _, t := range s.evidence {
			if !t.Time.After(b.f.B) {
				d.evidence = append(d.evidence, findingEvidence(t))
			}
		}
		for _, e := range b.f.Events {
			at := e.GetTime().AsTime()
			if (e.GetKind() == "stream.quorum_lost" || e.GetKind() == "consumer.quorum_lost") && sameAsset(e, s.t.Entity) &&
				!at.Before(s.start.Add(-2*b.f.Interval)) && !at.After(stop) {
				d.evidence = append(d.evidence, eventEvidence(e))
			}
		}
	}
	d.time = latest(start, b.f.A)
	if open {
		d.openAtB = true
	} else {
		d.end = end
	}
	check := first.t.Check
	switch {
	case check == "system-002":
		d.title = "No JetStream meta leader"
	case len(grp) == 1 && check == "stream-001":
		d.title = "Stream " + entityLabel(first.t.Entity) + " has no quorum"
	case len(grp) == 1 && check == "consumer-001":
		d.title = "Consumer " + entityLabel(first.t.Entity) + " has no quorum"
	case len(grp) == 1:
		d.title = checkTitle(first.t) + ": " + entityLabel(first.t.Entity)
	default:
		d.title = fmt.Sprintf("%s have no quorum", Plural(len(grp), entityNoun(first.t.Entity.GetType())))
	}
	if !open {
		d.title += " for " + durText(end.Sub(start))
	}
	var when string
	switch {
	case start.Before(b.f.A) && open:
		when = fmt.Sprintf("Already without quorum at A (since %s); still without quorum at B.", clockText(start))
	case start.Before(b.f.A):
		when = fmt.Sprintf("Already without quorum at A (since %s), until %s.", clockText(start), clockText(end))
	case open:
		when = fmt.Sprintf("From %s; still without quorum at B.", clockText(start))
	default:
		when = fmt.Sprintf("From %s to %s.", clockText(start), clockText(end))
	}
	if check == "system-002" {
		when = strings.ReplaceAll(when, "without quorum", "without a meta leader")
	}
	if len(grp) > 1 {
		d.body = joinSentences(listText(labels)+".", when)
	} else {
		d.body = when
	}
	b.add(d)
}

// findings adds a step per check for the warning and critical firings and
// changes between A and B that no other step took in, folded across
// entities within the fold window.
func (b *replayBuilder) findings() {
	var trs []ReplayTransition
	for _, id := range b.findingIDs() {
		all := b.byFinding[id]
		if quorumChecks[all[0].Check] || b.absorbed[id] {
			continue
		}
		for _, t := range all {
			if t.State != v1.FindingState_FINDING_STATE_RESOLVED && t.Severity >= v1.Severity_SEVERITY_WARNING && b.inRange(t.Time) &&
				!b.takenIn[transitionKey(t)] {
				trs = append(trs, t)
			}
		}
	}
	at := func(t ReplayTransition) time.Time { return t.Time }
	for _, grp := range foldGroups(trs, func(t ReplayTransition) string { return t.Check }, at, b.window) {
		d := &draft{kind: v1.ReplayStepKind_REPLAY_STEP_KIND_FINDING, time: grp[0].Time}
		labels, seen := []string{}, map[string]bool{}
		for _, t := range grp {
			b.hasStep[t.FindingID] = true
			d.severity = worse(d.severity, t.Severity)
			d.evidence = append(d.evidence, findingEvidence(t))
			if !seen[t.FindingID] {
				seen[t.FindingID] = true
				d.entities = append(d.entities, t.Entity)
				labels = append(labels, entityLabel(t.Entity))
			}
		}
		last := grp[len(grp)-1]
		if len(labels) == 1 {
			d.title = checkTitle(last) + ": " + labels[0]
			d.body = last.Detail
		} else {
			d.title = fmt.Sprintf("%s for %s", checkTitle(last), Plural(len(labels), entityNoun(last.Entity.GetType())))
			d.body = listText(labels) + "."
		}
		b.add(d)
	}
}

// resolves adds the resolves of warning and critical findings between A
// and B, and the first resolve after B of each finding with a step,
// folded by check; quorum findings resolve inside their span step.
func (b *replayBuilder) resolves() {
	var in, after []ReplayTransition
	for _, id := range b.findingIDs() {
		all := b.byFinding[id]
		quorum := quorumChecks[all[0].Check]
		doneAfter := false
		for _, t := range all {
			if t.State != v1.FindingState_FINDING_STATE_RESOLVED || t.Severity < v1.Severity_SEVERITY_WARNING {
				continue
			}
			switch {
			case b.inRange(t.Time) && !quorum:
				in = append(in, t)
			case b.afterB(t.Time) && b.hasStep[id] && !doneAfter:
				doneAfter = true
				after = append(after, t)
			}
		}
	}
	at := func(t ReplayTransition) time.Time { return t.Time }
	key := func(t ReplayTransition) string { return t.Check }
	for _, set := range []struct {
		trs     []ReplayTransition
		outside bool
	}{{in, false}, {after, true}} {
		for _, grp := range foldGroups(set.trs, key, at, b.window) {
			d := &draft{kind: v1.ReplayStepKind_REPLAY_STEP_KIND_RESOLVED, time: grp[0].Time, outside: set.outside}
			var labels []string
			for _, t := range grp {
				d.entities = append(d.entities, t.Entity)
				labels = append(labels, entityLabel(t.Entity))
				d.evidence = append(d.evidence, findingEvidence(t))
			}
			last := grp[len(grp)-1]
			name := checkTitle(last)
			if quorumChecks[last.Check] {
				name = map[string]string{"stream-001": "Stream quorum", "consumer-001": "Consumer quorum", "system-002": "Meta leader"}[last.Check] +
					" back"
				if len(grp) == 1 {
					d.title = name + ": " + labels[0]
				} else {
					d.title = fmt.Sprintf("%s for %s", name, Plural(len(grp), entityNoun(last.Entity.GetType())))
				}
			} else if len(grp) == 1 {
				d.title = name + " resolved: " + labels[0]
			} else {
				d.title = fmt.Sprintf("%s resolved for %s", name, Plural(len(grp), entityNoun(last.Entity.GetType())))
			}
			d.body = "Resolved at " + clockText(last.Time) + "."
			if len(grp) > 1 {
				d.body = joinSentences(listText(labels)+".", "Resolved from "+clockText(grp[0].Time)+" to "+clockText(last.Time)+".")
			}
			b.add(d)
		}
	}
}
