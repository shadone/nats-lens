package api

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// draft is a step being built.
type draft struct {
	kind        v1.ReplayStepKind
	time, end   time.Time
	openAtB     bool
	severity    v1.Severity
	title, body string
	entities    []*v1.EntityRef
	evidence    []*v1.ReplayEvidence
	outside     bool
}

// replayBuilder turns ReplayFacts into steps.
type replayBuilder struct {
	f *ReplayFacts
	// window is how close two steps with one fold key must be to fold:
	// the longer of 2 minutes and 2 collect intervals.
	window time.Duration
	steps  []*draft
	// used are the events a restart or stop step took in; absorbed the
	// findings whose transitions a gone step took in, takenIn the
	// transitions a restart step took in (by finding ID and time).
	used     map[string]bool
	absorbed map[string]bool
	takenIn  map[string]bool
	// hasStep are the findings with a warning or critical step in the
	// range; their first resolve after B closes it.
	hasStep map[string]bool
	// byFinding are the transitions of each finding in time order.
	byFinding map[string][]ReplayTransition
}

// FinishReplay words, folds, orders, caps and counts the facts a provider
// read into the response. The store and the demo both call it.
func FinishReplay(f *ReplayFacts) *v1.GetReplayResponse {
	b := &replayBuilder{f: f, window: max(2*time.Minute, 2*f.Interval), used: map[string]bool{}, absorbed: map[string]bool{}, takenIn: map[string]bool{},
		hasStep: map[string]bool{}, byFinding: map[string][]ReplayTransition{}}
	trs := slices.Clone(f.Transitions)
	slices.SortStableFunc(trs, func(x, y ReplayTransition) int { return x.Time.Compare(y.Time) })
	for _, t := range trs {
		b.byFinding[t.FindingID] = append(b.byFinding[t.FindingID], t)
	}
	slices.SortStableFunc(f.Events, func(x, y *v1.Event) int {
		return cmp.Or(x.GetTime().AsTime().Compare(y.GetTime().AsTime()), cmp.Compare(x.GetId(), y.GetId()))
	})
	b.restarts()
	b.stops()
	gone := b.gone()
	b.quorum()
	b.findings()
	b.resolves()
	b.k8s()
	b.leaders()
	b.gaps()
	res := &v1.GetReplayResponse{
		A: ts(f.A), B: ts(f.B), EpochA: f.EpochA, EpochAAt: tsOrNil(f.EpochAAt), EpochB: f.EpochB, EpochBAt: tsOrNil(f.EpochBAt),
		Windows: f.Windows, KubernetesWatched: f.Kubernetes, HistoryStart: tsOrNil(f.HistoryStart),
	}
	res.Steps, res.StepsOmitted = b.finish()
	res.StepsTruncated = res.StepsOmitted > 0
	res.Gone, res.GoneTruncated = gone, len(gone) > maxGoneAssets
	if res.GoneTruncated {
		res.Gone = gone[:maxGoneAssets]
	}
	res.Counts = b.counts()
	return res
}

func ts(t time.Time) *timestamppb.Timestamp { return timestamppb.New(t) }

func tsOrNil(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}

// inRange reports whether t lies between A and B, both included.
func (b *replayBuilder) inRange(t time.Time) bool { return !t.Before(b.f.A) && !t.After(b.f.B) }

// afterB reports whether t lies after B, up to Until.
func (b *replayBuilder) afterB(t time.Time) bool { return t.After(b.f.B) && !t.After(b.f.Until) }

func (b *replayBuilder) add(d *draft) { b.steps = append(b.steps, d) }

// evidence builders.
func eventEvidence(e *v1.Event) *v1.ReplayEvidence {
	return &v1.ReplayEvidence{Kind: v1.ReplayEvidenceKind_REPLAY_EVIDENCE_KIND_EVENT, Id: e.GetId(), Time: e.GetTime(), Label: e.GetSummary()}
}

func collectionEvidence(epoch int64, at time.Time, e *v1.EntityRef) *v1.ReplayEvidence {
	if epoch == 0 {
		return nil
	}
	return &v1.ReplayEvidence{Kind: v1.ReplayEvidenceKind_REPLAY_EVIDENCE_KIND_COLLECTION, Id: strconv.FormatInt(epoch, 10),
		Time: tsOrNil(at), Label: "collection of " + clockText(at), Entity: e}
}

func findingEvidence(t ReplayTransition) *v1.ReplayEvidence {
	return &v1.ReplayEvidence{Kind: v1.ReplayEvidenceKind_REPLAY_EVIDENCE_KIND_FINDING, Id: t.FindingID, Time: ts(t.Time),
		Label: fmt.Sprintf("%s %s (%s)", t.Check, checkTitle(t), stateWord(t.State))}
}

func serverEntity(name string) *v1.EntityRef {
	return &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SERVER, Key: name, Name: name, Server: name}
}

// eventEntity is the stream or consumer an event names; nil when it names
// none.
func eventEntity(e *v1.Event) *v1.EntityRef {
	switch {
	case e.GetStream() == "":
		return nil
	case e.GetConsumer() == "":
		return &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_STREAM, Key: e.GetAccount() + "/" + e.GetStream(), Name: e.GetStream(),
			Account: e.GetAccount(), Stream: e.GetStream()}
	}
	return &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_CONSUMER, Key: e.GetAccount() + "/" + e.GetStream() + "/" + e.GetConsumer(),
		Name: e.GetConsumer(), Account: e.GetAccount(), Stream: e.GetStream(), Consumer: e.GetConsumer()}
}

// sameAsset reports whether an event names an asset: the same stream and
// consumer, and the asset's account by ID or name or no account, as
// consumer-013 matches them.
func sameAsset(e *v1.Event, ref *v1.EntityRef) bool {
	return e.GetStream() == ref.GetStream() && e.GetConsumer() == ref.GetConsumer() &&
		(e.GetAccount() == "" || e.GetAccount() == ref.GetAccount() || (ref.GetAccountName() != "" && e.GetAccount() == ref.GetAccountName()))
}

// announcement is the first lame duck or shutdown event of a server
// between from and to that no other step took in, the lame duck first.
// Every such event in the span is marked used, so that a later restart or
// stop of the server does not take in the same announcement.
func (b *replayBuilder) announcement(server string, from, to time.Time) *v1.Event {
	var lame, shutdown *v1.Event
	for _, e := range b.f.Events {
		at := e.GetTime().AsTime()
		if e.GetServer() != server || at.Before(from) || at.After(to) || b.used[e.GetId()] {
			continue
		}
		switch e.GetKind() {
		case "server.lameduck":
			if lame == nil {
				lame = e
			}
		case "server.shutdown":
			if shutdown == nil {
				shutdown = e
			}
		default:
			continue
		}
		b.used[e.GetId()] = true
	}
	if lame != nil {
		return lame
	}
	return shutdown
}

// podEvents are the unused NATS pod events of a server between from and
// to; they are marked used.
func (b *replayBuilder) podEvents(server string, from, to time.Time) []*v1.Event {
	var out []*v1.Event
	for _, e := range b.f.Events {
		at := e.GetTime().AsTime()
		if e.GetServer() == server && slices.Contains(ReplayPodKinds, e.GetKind()) && !b.used[e.GetId()] && !at.Before(from) && !at.After(to) {
			b.used[e.GetId()] = true
			out = append(out, e)
		}
	}
	return out
}

// restarts adds a step per replaced process: its span from the old
// process's last answer to the new one's first, the announcement, the
// pod events, the leaders elected elsewhere and the restart findings.
func (b *replayBuilder) restarts() {
	rs := slices.Clone(b.f.Restarts)
	slices.SortStableFunc(rs, func(x, y ReplayRestart) int { return x.LastSeen.Compare(y.LastSeen) })
	for _, r := range rs {
		started := cmp.Or(r.Started, r.FirstSeen)
		d := &draft{kind: v1.ReplayStepKind_REPLAY_STEP_KIND_RESTART, time: r.LastSeen, end: r.FirstSeen,
			entities: []*v1.EntityRef{serverEntity(r.Server)}, title: r.Server + " restarts"}
		if r.OldVersion != "" && r.NewVersion != "" && r.OldVersion != r.NewVersion {
			d.title = fmt.Sprintf("%s restarts for an upgrade from %s to %s", r.Server, r.OldVersion, r.NewVersion)
		}
		ann := b.announcement(r.Server, r.LastSeen.Add(-b.f.RestartWindow), started)
		if ann != nil {
			b.used[ann.GetId()] = true
			d.evidence = append(d.evidence, eventEvidence(ann))
		}
		for _, e := range b.podEvents(r.Server, r.LastSeen.Add(-b.f.RestartWindow), r.FirstSeen.Add(2*b.f.Interval)) {
			d.evidence = append(d.evidence, eventEvidence(e))
		}
		d.evidence = append(d.evidence, collectionEvidence(r.OldEpoch, r.LastSeen, serverEntity(r.Server)),
			collectionEvidence(r.NewEpoch, r.FirstSeen, serverEntity(r.Server)))
		from := r.LastSeen
		if ann != nil {
			from = ann.GetTime().AsTime()
		}
		d.body = joinSentences(announcementText(ann, b.f.EventsCaptured), assetsAfterText(r),
			b.leadersTaken(r, from, r.FirstSeen.Add(b.f.Interval), d))
		for _, id := range b.findingIDs() {
			for _, t := range b.byFinding[id] {
				if t.Check == restartCheck && t.Entity.GetKey() == r.Server && !t.Time.Before(r.LastSeen) && !t.Time.After(r.FirstSeen.Add(b.f.Interval)) {
					b.takenIn[transitionKey(t)] = true
					d.evidence = append(d.evidence, findingEvidence(t))
				}
			}
		}
		b.add(d)
	}
}

// leadersTaken marks the elections between from and to of the assets the
// restarted process led, won by another server, as used and says where
// they went: "Leaders it held were elected on other servers: 3 on nats-2,
// 1 on nats-5."
func (b *replayBuilder) leadersTaken(r ReplayRestart, from, to time.Time, d *draft) string {
	by := map[string]int{}
	for _, e := range b.f.Events {
		at := e.GetTime().AsTime()
		if (e.GetKind() != "stream.leader_elected" && e.GetKind() != "consumer.leader_elected") || b.used[e.GetId()] ||
			e.GetServer() == r.Server || at.Before(from) || at.After(to) {
			continue
		}
		if !slices.ContainsFunc(r.LedBefore, func(ref *v1.EntityRef) bool { return sameAsset(e, ref) }) {
			continue
		}
		b.used[e.GetId()] = true
		by[e.GetServer()]++
		d.evidence = append(d.evidence, eventEvidence(e))
	}
	if len(by) == 0 {
		return ""
	}
	servers := slices.Sorted(func(yield func(string) bool) {
		for s := range by {
			if !yield(s) {
				return
			}
		}
	})
	slices.SortStableFunc(servers, func(x, y string) int { return cmp.Compare(by[y], by[x]) })
	parts := make([]string, len(servers))
	for i, s := range servers {
		parts[i] = fmt.Sprintf("%d on %s", by[s], s)
	}
	return "Leaders it held were elected on other servers: " + strings.Join(parts, ", ") + "."
}

// stops adds a step per server that stopped answering and was not back
// by B, and an outside-range step when it answered again before Until.
func (b *replayBuilder) stops() {
	for _, s := range b.f.Stops {
		d := &draft{kind: v1.ReplayStepKind_REPLAY_STEP_KIND_RESTART, time: s.LastSeen, openAtB: true,
			severity: v1.Severity_SEVERITY_WARNING, entities: []*v1.EntityRef{serverEntity(s.Server)},
			title: s.Server + " stops answering"}
		ann := b.announcement(s.Server, s.LastSeen.Add(-b.f.RestartWindow), latest(s.LastSeen.Add(b.f.RestartWindow), b.f.B))
		if ann != nil {
			b.used[ann.GetId()] = true
			d.evidence = append(d.evidence, eventEvidence(ann))
		}
		for _, e := range b.podEvents(s.Server, s.LastSeen.Add(-b.f.RestartWindow), b.f.B) {
			d.evidence = append(d.evidence, eventEvidence(e))
		}
		d.evidence = append(d.evidence, collectionEvidence(s.LastEpoch, s.LastSeen, serverEntity(s.Server)),
			collectionEvidence(b.f.EpochB, b.f.EpochBAt, nil))
		d.body = joinSentences(announcementText(ann, b.f.EventsCaptured),
			fmt.Sprintf("Last answered at %s; not answering at B.", clockText(s.LastSeen)))
		b.add(d)
		if !s.BackAt.IsZero() && b.afterB(s.BackAt) {
			b.add(&draft{kind: v1.ReplayStepKind_REPLAY_STEP_KIND_RESOLVED, time: s.BackAt, outside: true,
				entities: []*v1.EntityRef{serverEntity(s.Server)}, title: s.Server + " answers again",
				body:     fmt.Sprintf("A process of %s answered at %s.", s.Server, clockText(s.BackAt)),
				evidence: []*v1.ReplayEvidence{collectionEvidence(s.BackEpoch, s.BackAt, serverEntity(s.Server))}})
		}
	}
}

// transitionKey identifies a transition: its finding and time.
func transitionKey(t ReplayTransition) string {
	return t.FindingID + "@" + t.Time.Format(time.RFC3339Nano)
}

// findingIDs are the IDs of the findings with transitions, sorted.
func (b *replayBuilder) findingIDs() []string { return slices.Sorted(mapKeys(b.byFinding)) }

func latest(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// foldGroups groups items in time order: an item joins the newest group
// of its key when it comes within window of that group's last item.
func foldGroups[T any](xs []T, key func(T) string, at func(T) time.Time, window time.Duration) [][]T {
	sorted := slices.Clone(xs)
	slices.SortStableFunc(sorted, func(x, y T) int { return at(x).Compare(at(y)) })
	open := map[string]int{}
	var groups [][]T
	for _, x := range sorted {
		k := key(x)
		if i, ok := open[k]; ok && at(x).Sub(at(groups[i][len(groups[i])-1])) <= window {
			groups[i] = append(groups[i], x)
			continue
		}
		open[k] = len(groups)
		groups = append(groups, []T{x})
	}
	return groups
}

func worse(a, b v1.Severity) v1.Severity { return max(a, b) }
