package notify

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/shadone/nats-lens/internal/checks"
	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/store"
)

// Reasons a message gives when its kind alone does not say why.
const (
	ReasonMuteEnded       = "mute ended"
	ReasonAcceptanceEnded = "acceptance ended"
	ReasonFlappingEnded   = "flapping ended"
	ReasonFiltersChanged  = "no longer selected by the channel"
)

// ReasonSeverityNow is the reason of a resolved message for a finding
// whose severity fell below the channel's min_severity.
func ReasonSeverityNow(severity string) string { return "severity now " + severity }

// Reconciler decides, after each check run of one system, what each
// channel is to be sent, and queues it in the system's store. It sends
// nothing itself.
type Reconciler struct {
	Store      *store.Store
	SystemID   string
	SystemName string
	// Channels are every configured channel; those whose systems leave
	// this system out are skipped.
	Channels []config.Channel
	// Checks name the checks and their categories.
	Checks *checks.Set
	// Retention is the system's retention, which bounds how long sent
	// messages are kept (store.SweepNotifications).
	Retention time.Duration
	// Log receives what was queued; nil uses slog.Default().
	Log *slog.Logger
}

func (r *Reconciler) logger() *slog.Logger {
	if r.Log == nil {
		return slog.Default()
	}
	return r.Log
}

// channels are the configured channels that receive this system.
func (r *Reconciler) channels() []config.Channel {
	var out []config.Channel
	for _, c := range r.Channels {
		if len(c.Systems) == 0 || slices.Contains(c.Systems, r.SystemID) {
			out = append(out, c)
		}
	}
	return out
}

// ReconcileResult counts what one reconcile queued.
type ReconcileResult struct {
	// Queued counts the outbox rows by channel.
	Queued map[string]int
	// Baselined are the channels that got their baseline, in the
	// configuration's order.
	Baselined []string
}

// Reconcile compares each channel's view at the epoch with what it was
// last told, and queues the difference in one transaction. A channel that
// was reconciled for the epoch already is left alone.
func (r *Reconciler) Reconcile(ctx context.Context, epoch int64) (ReconcileResult, error) {
	res := ReconcileResult{Queued: map[string]int{}}
	chans := r.channels()
	if len(chans) == 0 {
		return res, nil
	}
	in, err := r.Store.NotifyInput(ctx, epoch)
	if err != nil {
		return res, fmt.Errorf("read notification state: %w", err)
	}
	var change store.NotifyChange
	for _, c := range chans {
		prev, known := in.Channels[c.Name]
		if known && prev.ReconciledEpoch >= epoch {
			continue
		}
		d := &decider{r: r, in: in, ch: c, prev: prev, states: in.States[c.Name]}
		if !known {
			d.baseline()
			res.Baselined = append(res.Baselined, c.Name)
			change.Channels = append(change.Channels, store.NotifyChannel{Name: c.Name, BaselinedAt: in.At, BaselinedEpoch: epoch,
				ReconciledAt: in.At, ReconciledEpoch: epoch})
		} else {
			d.compare()
			prev.ReconciledAt, prev.ReconciledEpoch = in.At, epoch
			change.Channels = append(change.Channels, prev)
		}
		rows, err := d.outbox()
		if err != nil {
			return res, err
		}
		change.Upserts = append(change.Upserts, d.upserts...)
		change.Deletes = append(change.Deletes, d.deletes...)
		change.Outbox = append(change.Outbox, rows...)
		if len(rows) > 0 {
			res.Queued[c.Name] = len(rows)
		}
	}
	if len(change.Channels) == 0 {
		return res, nil
	}
	if err := r.Store.ApplyNotify(ctx, change); err != nil {
		return res, fmt.Errorf("queue notifications: %w", err)
	}
	for _, name := range res.Baselined {
		r.logger().Info("notification channel baselined", "epoch", epoch, "channel", name)
	}
	for _, c := range chans {
		if n := res.Queued[c.Name]; n > 0 {
			r.logger().Info("notifications queued", "epoch", epoch, "channel", c.Name, "messages", n)
		}
	}
	return res, nil
}

// Sweep removes old sent messages and the rows of channels no longer
// configured for this system (store.SweepNotifications), and logs the
// channels it removed.
func (r *Reconciler) Sweep(ctx context.Context, now time.Time) error {
	var keep []string
	for _, c := range r.channels() {
		keep = append(keep, c.Name)
	}
	res, err := r.Store.SweepNotifications(ctx, now, r.Retention, keep)
	if err != nil {
		return err
	}
	for _, name := range res.Channels {
		r.logger().Info("notification channel no longer configured: its state and queued messages removed", "channel", name)
	}
	if res.Dropped > 0 {
		r.logger().Warn("queued notifications of removed channels dropped", "messages", res.Dropped)
	}
	return nil
}

// decider works out one channel's messages for one epoch.
type decider struct {
	r      *Reconciler
	in     store.NotifyInput
	ch     config.Channel
	prev   store.NotifyChannel
	states map[string]store.NotifyState

	msgs    []queued
	upserts []store.NotifyState
	deletes []store.NotifyKey
}

// queued is one message, or the messages about one finding that go
// together in this order; the last one places them in the batch.
type queued struct {
	msgs []Message
}

func (q queued) key() Message { return q.msgs[len(q.msgs)-1] }

// check is the check's name and category; empty for a check this binary
// does not have.
func (d *decider) check(code string) (name, category string) {
	if d.r.Checks == nil {
		return "", ""
	}
	if c, ok := d.r.Checks.Lookup(code); ok {
		return c.Name, c.Category
	}
	return "", ""
}

// filtersPass reports whether the channel's filters other than
// min_severity take the finding.
func (d *decider) filtersPass(f store.Finding) bool {
	_, cat := d.check(f.CheckCode)
	return d.ch.Accepts(d.r.SystemID, f.CheckCode, cat, d.ch.MinSeverity)
}

func (d *decider) severityPasses(sev string) bool {
	return config.SeverityRank(sev) >= config.SeverityRank(d.ch.MinSeverity)
}

// eligible reports whether the finding, firing or not, would be in the
// view: no mute in force, and every filter takes it.
func (d *decider) eligible(f store.Finding) bool {
	return f.Mute == nil && d.filtersPass(f) && d.severityPasses(f.Severity)
}

func (d *decider) inView(f store.Finding) bool { return f.Firing() && d.eligible(f) }

// ids are the findings the channel sees or knows, sorted.
func (d *decider) ids() []string {
	var out []string
	for id := range d.in.Findings {
		out = append(out, id)
	}
	for id := range d.states {
		if _, ok := d.in.Findings[id]; !ok {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

// baseline records the channel's view as told, and queues the one
// baseline message.
func (d *decider) baseline() {
	n := 0
	for _, id := range d.ids() {
		f, ok := d.in.Findings[id]
		if !ok || !d.inView(f) {
			continue
		}
		n++
		state := store.NotifyFiring
		if f.Flapping {
			state = store.NotifyFlapping
		}
		d.told(f, state)
	}
	d.msgs = append(d.msgs, queued{msgs: []Message{BaselineMessage(d.r.SystemID, n, d.in.Epoch)}})
}

// told records that the channel now knows the finding in state.
func (d *decider) told(f store.Finding, state string) {
	d.upserts = append(d.upserts, store.NotifyState{Channel: d.ch.Name, FindingID: f.ID, SinceEpoch: f.SinceEpoch,
		Severity: f.Severity, State: state, SentAt: d.in.At, Epoch: d.in.Epoch})
}

func (d *decider) forget(id string) {
	d.deletes = append(d.deletes, store.NotifyKey{Channel: d.ch.Name, FindingID: id})
}

// compare queues the difference between the view and what was told.
func (d *decider) compare() {
	for _, id := range d.ids() {
		f, ok := d.in.Findings[id]
		st, told := d.states[id]
		var fp *store.Finding
		if ok {
			fp = &f
		}
		d.one(fp, st, told)
	}
}

// one decides for one finding: f is nil when the finding is gone from the
// store; told says whether st holds what the channel was told.
func (d *decider) one(f *store.Finding, st store.NotifyState, told bool) {
	if f != nil && f.Flapping && d.eligible(*f) {
		switch {
		case told && st.State == store.NotifyFlapping:
			// Held back while it flaps.
		case told || f.Firing():
			d.send(f, KindFlapping, "", nil)
			d.told(*f, store.NotifyFlapping)
		}
		return
	}
	if !told {
		if f != nil && d.inView(*f) {
			reason, mute := d.muteEnded(*f)
			d.send(f, KindFiring, reason, func(m *Message) { m.Mute = mute })
			d.told(*f, store.NotifyFiring)
		}
		return
	}
	wasFlapping := st.State == store.NotifyFlapping
	switch {
	case f == nil:
		d.sendGone(st)
		d.forget(st.FindingID)
	case f.Firing() && f.Mute != nil:
		mu := muteOf(*f.Mute)
		d.send(f, KindMuted, "", func(m *Message) { m.Mute = mu })
		d.forget(f.ID)
	case !f.Firing():
		d.send(f, KindResolved, "", func(m *Message) {
			m.ResolvedAt = f.ResolvedAt
			m.DedupKey = DedupKey(d.r.SystemID, f.ID, st.SinceEpoch, KindResolved, d.in.Epoch)
		})
		d.forget(f.ID)
	case !d.filtersPass(*f):
		d.send(f, KindResolved, ReasonFiltersChanged, func(m *Message) { m.DedupKey = d.episodeKey(f.ID, st, KindResolved) })
		d.forget(f.ID)
	case !d.severityPasses(f.Severity):
		d.send(f, KindResolved, ReasonSeverityNow(f.Severity), func(m *Message) {
			m.PreviousSeverity = st.Severity
			m.DedupKey = d.episodeKey(f.ID, st, KindResolved)
		})
		d.forget(f.ID)
	case wasFlapping:
		d.send(f, KindFiring, ReasonFlappingEnded, nil)
		d.told(*f, store.NotifyFiring)
	case f.SinceEpoch != st.SinceEpoch:
		// The finding resolved and fired again while no reconcile ran: the
		// old episode is closed, then the new one opened.
		closed := d.message(*f, KindResolved)
		closed.Severity, closed.Detail, closed.Value = st.Severity, "", nil
		closed.Since, closed.TransitionedAt, closed.ConditionSince = time.Time{}, time.Time{}, time.Time{}
		closed.DedupKey = d.episodeKey(f.ID, st, KindResolved)
		d.msgs = append(d.msgs, queued{msgs: []Message{closed, d.message(*f, KindFiring)}})
		d.told(*f, store.NotifyFiring)
	case f.Severity != st.Severity:
		d.send(f, KindChanged, "", func(m *Message) { m.PreviousSeverity = st.Severity })
		d.told(*f, store.NotifyFiring)
	case d.ch.RepeatEvery.D() > 0 && !d.in.At.Before(st.SentAt.Add(d.ch.RepeatEvery.D())):
		d.send(f, KindReminder, "", nil)
		d.told(*f, store.NotifyFiring)
	}
}

// episodeKey is the dedup key of a message about the episode the channel
// was told of.
func (d *decider) episodeKey(id string, st store.NotifyState, kind Kind) string {
	return DedupKey(d.r.SystemID, id, st.SinceEpoch, kind, d.in.Epoch)
}

// muteEnded finds the mute or acceptance that covered the finding and
// ended after the channel's previous reconcile and by the epoch, the
// latest to end; the reason and the mute are empty without one.
func (d *decider) muteEnded(f store.Finding) (string, *Mute) {
	var best *store.Mute
	var bestEnd time.Time
	for i := range d.in.Mutes {
		m := d.in.Mutes[i]
		if m.CheckCode != f.CheckCode || (m.EntityType != "" && (m.EntityType != f.EntityType || m.EntityKey != f.EntityKey)) {
			continue
		}
		end := m.ExpiresAt
		if !m.UnmutedAt.IsZero() && m.UnmutedAt.Before(end) {
			end = m.UnmutedAt
		}
		if !end.After(d.prev.ReconciledAt) || end.After(d.in.At) || !end.After(f.Since) || end.Before(m.CreatedAt) {
			continue
		}
		if best == nil || end.After(bestEnd) {
			best, bestEnd = &m, end
		}
	}
	if best == nil {
		return "", nil
	}
	if best.Accepted {
		return ReasonAcceptanceEnded, muteOf(*best)
	}
	return ReasonMuteEnded, muteOf(*best)
}

func muteOf(m store.Mute) *Mute {
	return &Mute{Accepted: m.Accepted, By: m.MutedBy, Reason: m.Reason, Until: m.ExpiresAt, EndReason: m.EndReason}
}

// send queues a message about f of the kind; edit, when set, adjusts it.
func (d *decider) send(f *store.Finding, kind Kind, reason string, edit func(*Message)) {
	m := d.message(*f, kind)
	m.Reason = reason
	if edit != nil {
		edit(&m)
	}
	d.msgs = append(d.msgs, queued{msgs: []Message{m}})
}

// sendGone queues the resolve of a finding the store no longer holds,
// from what the channel was told.
func (d *decider) sendGone(st store.NotifyState) {
	code, key, _ := strings.Cut(st.FindingID, ":")
	name, cat := d.check(code)
	m := Message{Kind: KindResolved, FindingID: st.FindingID, CheckCode: code, CheckName: name, Category: cat,
		Severity: st.Severity, EntityKey: key, DedupKey: d.episodeKey(st.FindingID, st, KindResolved)}
	d.msgs = append(d.msgs, queued{msgs: []Message{m}})
}

// message is the message of the kind about f, as the Findings page words
// the finding.
func (d *decider) message(f store.Finding, kind Kind) Message {
	name, cat := d.check(f.CheckCode)
	m := Message{
		Kind: kind, FindingID: f.ID, CheckCode: f.CheckCode, CheckName: name, Category: cat, Severity: f.Severity,
		EntityType: f.EntityType, EntityKey: f.EntityKey, EntityName: entityName(f.EntityType, f.EntityKey, d.in.AccountNames),
		Detail: f.Detail, Since: f.Since, TransitionedAt: f.TransitionedAt, Flapping: f.Flapping, Value: f.Value,
		DedupKey: DedupKey(d.r.SystemID, f.ID, f.SinceEpoch, kind, d.in.Epoch),
	}
	if f.ConditionSince != nil {
		m.ConditionSince = *f.ConditionSince
	}
	return m
}

// entityName is the entity's key with its account ID replaced by the
// account's readable name, as the UI labels it; empty when no part has a
// name.
func entityName(typ, key string, names map[string]string) string {
	switch typ {
	case "account":
		return names[key]
	case "stream", "consumer":
		acc, rest, ok := strings.Cut(key, "/")
		if n := names[acc]; ok && n != "" {
			return n + "/" + rest
		}
	}
	return ""
}

// outbox is the channel's messages in the Findings page's order, worst
// first and resolves last, as outbox rows; a kind the channel does not
// take is left out.
func (d *decider) outbox() ([]store.OutboxRow, error) {
	slices.SortStableFunc(d.msgs, compareQueued)
	var out []store.OutboxRow
	for _, q := range d.msgs {
		for _, m := range q.msgs {
			if !d.ch.Sends(string(m.Kind)) {
				continue
			}
			b, err := m.Encode()
			if err != nil {
				return nil, fmt.Errorf("encode %s message for %s: %w", m.Kind, m.FindingID, err)
			}
			out = append(out, store.OutboxRow{Channel: d.ch.Name, CreatedAt: d.in.At, Epoch: d.in.Epoch, Kind: string(m.Kind),
				FindingID: m.FindingID, DedupKey: m.DedupKey, Message: b})
		}
	}
	return out, nil
}

// kindGroup puts what still fires first, then mutes, then resolves.
func kindGroup(k Kind) int {
	switch k {
	case KindMuted:
		return 1
	case KindResolved:
		return 2
	}
	return 0
}

// compareQueued orders messages as api.CompareWorst orders findings:
// firing before muted before resolved, then by severity, an expiry
// warning after the other warnings, the longest firing first, then by ID.
func compareQueued(qa, qb queued) int {
	a, b := qa.key(), qb.key()
	return cmp.Or(
		cmp.Compare(kindGroup(a.Kind), kindGroup(b.Kind)),
		cmp.Compare(config.SeverityRank(b.Severity), config.SeverityRank(a.Severity)),
		cmp.Compare(expiryRank(a), expiryRank(b)),
		a.Since.Compare(b.Since),
		cmp.Compare(a.FindingID, b.FindingID),
	)
}

func expiryRank(m Message) int {
	if m.Severity == "warning" && checks.IsExpiry(m.CheckCode) {
		return 1
	}
	return 0
}
