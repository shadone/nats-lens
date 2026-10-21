package api

import (
	"cmp"
	"slices"
	"strings"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// Subjects limits and rules.
const (
	// MaxSubjectRows is the most rows a GetSubjects response lists.
	MaxSubjectRows = 1000
	// MaxClientNames is the most connection names a row or match lists.
	MaxClientNames = 3
	// MaxSubjectLength is the longest subject MatchSubject takes, in bytes.
	MaxSubjectLength = 255
	// SubscriptionsMaxAge is how far before the epoch the subscription
	// collection a page reads may lie.
	SubscriptionsMaxAge = 24 * time.Hour
	// RateSpan is how far before the range's end the stored rate and the
	// JetStream API rate look.
	RateSpan = 15 * time.Minute
	// InboxPrefix starts every reply inbox subject; InboxPattern is the
	// pattern of the row they fold into.
	InboxPrefix  = "_INBOX."
	InboxPattern = "_INBOX.>"
	// JetStreamAPIPattern is the pattern of an account's JetStream API row.
	JetStreamAPIPattern = "$JS.API.>"
)

// SubjectsFacts are what a provider read for one GetSubjects: the epoch's
// streams with their consumers, the JetStream API rates of the accounts
// that use JetStream, and the subscription collection grouped by
// GroupSubscriptions' rules. FinishSubjects turns them into the response,
// so the store and the demo apply the same rules. A provider may leave out
// what lies outside the request's account; FinishSubjects scopes again.
type SubjectsFacts struct {
	// Epoch is the range's newest epoch; 0 when the range holds none.
	Epoch   int64
	EpochAt time.Time
	// SystemAccount is the system account's identifier; empty when not
	// known.
	SystemAccount string
	Streams       []SubjectStreamFact
	// APIs are the accounts with JetStream use, one each.
	APIs []JetStreamAPIFact
	// Subscriptions is nil when no collection lies within
	// SubscriptionsMaxAge at or before the epoch.
	Subscriptions *SubscriptionFacts
}

// SubjectStreamFact is one stream of the epoch: its configuration from
// the leader (else the first reporting server by name), its messages from
// the leader (else the largest any server reports), its consumers and its
// stored rate.
type SubjectStreamFact struct {
	Account, Stream, Retention, Mirror string
	Subjects                           []string
	// Sources are the streams it sources from, by name.
	Sources []string
	// Messages is nil when not reported.
	Messages *int64
	// StoredPerSecond is the increase of the leader's last sequence per
	// second over RateSpan before the range's end; nil without two reports.
	StoredPerSecond *float64
	Consumers       []SubjectConsumerFact
}

// SubjectConsumerFact is one consumer of a stream in the epoch.
type SubjectConsumerFact struct {
	Name    string
	Durable bool
	// Filters are its filter subjects; none means every subject.
	Filters []string
	// DeliverSubject is a push consumer's deliver subject; empty for a
	// pull consumer.
	DeliverSubject string
}

// JetStreamAPIFact is an account's JetStream API over RateSpan before the
// range's end, summed over the servers.
type JetStreamAPIFact struct {
	Account string
	// PerSecond is nil when no server reported twice; ErrorShare nil
	// besides when no request was made.
	PerSecond, ErrorShare *float64
}

// SubscriptionFacts are one subscription collection, grouped.
type SubscriptionFacts struct {
	// At is when the collection's epoch started.
	At time.Time
	// Servers are the servers asked, by name.
	Servers []SubszServerFact
	// Groups are the rows' subscription groups and Deliver the deliver
	// subjects' subscribers, as GroupSubscriptions builds them.
	Groups, Deliver []SubscriptionGroup
	// ServerSubscriptions and Unlisted count the subscriptions left out,
	// by account.
	ServerSubscriptions, Unlisted map[string]int64
}

// SubszServerFact is one server asked for its subscription list.
type SubszServerFact struct {
	Name     string
	Answered bool
	// Total and Listed are nil when it did not answer.
	Total, Listed *int64
}

// SubscriptionGroup is client subscriptions on one subject in one
// account: a row of the page, a deliver subject's subscribers, or a
// tester's subscribers on one subject and queue.
type SubscriptionGroup struct {
	Account, Subject string
	// Queue is the queue group every subscription of the group shares;
	// empty when they share none.
	Queue string
	// Connections counts the distinct connections (server and client ID).
	Connections int32
	// Names are distinct connection names in order; a provider may cut
	// them at MaxClientNames when NameCount keeps the total.
	Names     []string
	NameCount int32
}

// SubscriptionFact is one stored subscription row: the subscriptions of
// one connection on one subject and queue on one server.
type SubscriptionFact struct {
	Server                  string
	Account, Subject, Queue string
	CID                     uint64
	Subs                    int64
}

// ServerConnections is one server's CONNZ in the subscription
// collection's epoch: the client IDs it listed with their names, and
// whether it listed every connection (none was cut at the connz limit).
type ServerConnections struct {
	Complete bool
	Names    map[uint64]string
}

// GroupedSubscriptions are a subscription collection by GroupSubscriptions'
// rules.
type GroupedSubscriptions struct {
	Groups, Deliver               []SubscriptionGroup
	ServerSubscriptions, Unlisted map[string]int64
}

// connKey is a connection: its server and client ID.
type connKey struct {
	server string
	cid    uint64
}

// groupAcc gathers one group's subscriptions.
type groupAcc struct {
	g      SubscriptionGroup
	conns  map[connKey]bool
	names  map[string]bool
	queues map[string]bool
}

func (a *groupAcc) add(k connKey, name, queue string) {
	a.conns[k] = true
	if name != "" {
		a.names[name] = true
	}
	a.queues[queue] = true
}

func (a *groupAcc) done() SubscriptionGroup {
	g := a.g
	g.Connections = int32(len(a.conns))
	for n := range a.names {
		g.Names = append(g.Names, n)
	}
	slices.Sort(g.Names)
	g.NameCount = int32(len(g.Names))
	if len(a.queues) == 1 {
		for q := range a.queues {
			g.Queue = q
		}
	}
	return g
}

// GroupSubscriptions applies the page's rules to a subscription
// collection; the store's SQL follows the same rules. A subscription whose
// connection is not in its server's CONNZ is the server's own when that
// CONNZ listed every connection, else its connection is unlisted; both are
// counted by account and left out. Of the clients' subscriptions, those on
// a push consumer's deliver subject (deliver, by account and subject) are
// grouped by account and subject into Deliver; the others by account and
// subject, every _INBOX. subject folded into InboxPattern, into Groups.
func GroupSubscriptions(subs []SubscriptionFact, conns map[string]ServerConnections, deliver map[[2]string]bool) GroupedSubscriptions {
	out := GroupedSubscriptions{ServerSubscriptions: map[string]int64{}, Unlisted: map[string]int64{}}
	groups, del := map[[2]string]*groupAcc{}, map[[2]string]*groupAcc{}
	for _, s := range subs {
		c := conns[s.Server]
		name, client := c.Names[s.CID]
		switch {
		case !client && c.Complete:
			out.ServerSubscriptions[s.Account] += s.Subs
			continue
		case !client:
			out.Unlisted[s.Account] += s.Subs
			continue
		}
		key, into := [2]string{s.Account, s.Subject}, groups
		switch {
		case deliver[key]:
			into = del
		case strings.HasPrefix(s.Subject, InboxPrefix):
			key[1] = InboxPattern
		}
		a := into[key]
		if a == nil {
			a = &groupAcc{g: SubscriptionGroup{Account: key[0], Subject: key[1]}, conns: map[connKey]bool{}, names: map[string]bool{},
				queues: map[string]bool{}}
			into[key] = a
		}
		a.add(connKey{s.Server, s.CID}, name, s.Queue)
	}
	for _, a := range groups {
		out.Groups = append(out.Groups, a.done())
	}
	for _, a := range del {
		out.Deliver = append(out.Deliver, a.done())
	}
	sortGroups(out.Groups)
	sortGroups(out.Deliver)
	return out
}

// CoreGroups groups a subscription collection for the tester: the clients'
// subscriptions by account, subject and queue, with no fold; the servers'
// own and those of unlisted connections are left out.
func CoreGroups(subs []SubscriptionFact, conns map[string]ServerConnections) []SubscriptionGroup {
	groups := map[[3]string]*groupAcc{}
	for _, s := range subs {
		name, client := conns[s.Server].Names[s.CID]
		if !client {
			continue
		}
		key := [3]string{s.Account, s.Subject, s.Queue}
		a := groups[key]
		if a == nil {
			a = &groupAcc{g: SubscriptionGroup{Account: s.Account, Subject: s.Subject}, conns: map[connKey]bool{}, names: map[string]bool{},
				queues: map[string]bool{}}
			groups[key] = a
		}
		a.add(connKey{s.Server, s.CID}, name, s.Queue)
	}
	var out []SubscriptionGroup
	for _, a := range groups {
		out = append(out, a.done())
	}
	sortGroups(out)
	return out
}

func sortGroups(gs []SubscriptionGroup) {
	slices.SortFunc(gs, func(a, b SubscriptionGroup) int {
		return cmp.Or(cmp.Compare(a.Account, b.Account), cmp.Compare(a.Subject, b.Subject), cmp.Compare(a.Queue, b.Queue))
	})
}

// IsInbox reports whether a subject is a reply inbox.
func IsInbox(subject string) bool { return strings.HasPrefix(subject, InboxPrefix) }

// SubjectMatches reports whether a literal subject matches a subscribed
// subject or stream subject, wildcards included.
func SubjectMatches(literal, pattern string) bool {
	return natsserver.SubjectMatchesFilter(literal, pattern)
}

// SubjectsOverlap reports whether some literal subject matches both
// patterns.
func SubjectsOverlap(a, b string) bool { return natsserver.SubjectsCollide(a, b) }

// ValidateLiteralSubject checks a subject MatchSubject takes: a literal
// with no wildcard token, no empty token and no white space, at most
// MaxSubjectLength bytes.
func ValidateLiteralSubject(subject string) error {
	if subject == "" {
		return invalid("subject is required")
	}
	if len(subject) > MaxSubjectLength {
		return invalid("subject is longer than %d bytes", MaxSubjectLength)
	}
	for _, t := range strings.Split(subject, ".") {
		switch {
		case t == "":
			return invalid("subject %q has an empty token", subject)
		case t == "*" || t == ">":
			return invalid("subject %q has a wildcard; give a literal subject", subject)
		case strings.ContainsAny(t, " \t\r\n\f"):
			return invalid("subject %q has white space", subject)
		}
	}
	return nil
}

// SubjectKind is a stream's row kind by the prefix rule of
// ListKeyValueStores and ListObjectStores.
func SubjectKind(stream string) v1.SubjectRowKind {
	switch CapacityKind(stream) {
	case v1.CapacityStreamKind_CAPACITY_STREAM_KIND_KEY_VALUE:
		return v1.SubjectRowKind_SUBJECT_ROW_KIND_KEY_VALUE
	case v1.CapacityStreamKind_CAPACITY_STREAM_KIND_OBJECT_STORE:
		return v1.SubjectRowKind_SUBJECT_ROW_KIND_OBJECT_STORE
	}
	return v1.SubjectRowKind_SUBJECT_ROW_KIND_STREAM
}

// storedKind reports whether a row is a stream, KV bucket or object store.
func storedKind(k v1.SubjectRowKind) bool {
	switch k {
	case v1.SubjectRowKind_SUBJECT_ROW_KIND_STREAM, v1.SubjectRowKind_SUBJECT_ROW_KIND_KEY_VALUE,
		v1.SubjectRowKind_SUBJECT_ROW_KIND_OBJECT_STORE:
		return true
	}
	return false
}

// inScope reports whether an account lies in the request's scope.
func inScope(scope, account string) bool { return scope == "" || scope == account }

// cutNames keeps at most MaxClientNames of names in order; more is the
// count beyond them, of total names.
func cutNames(names []string, total int32) ([]string, int32) {
	names = slices.Clone(names)
	slices.Sort(names)
	names = slices.Compact(names)
	total = max(total, int32(len(names)))
	keep := names[:min(len(names), MaxClientNames)]
	return keep, total - int32(len(keep))
}

// streamLinks are, by account and stream, the streams that mirror it and
// those that source from it.
type streamLinks struct{ mirroredBy, sourcedInto map[[2]string][]string }

func linksOf(streams []SubjectStreamFact) streamLinks {
	l := streamLinks{mirroredBy: map[[2]string][]string{}, sourcedInto: map[[2]string][]string{}}
	for _, s := range streams {
		if s.Mirror != "" {
			k := [2]string{s.Account, s.Mirror}
			l.mirroredBy[k] = append(l.mirroredBy[k], s.Stream)
		}
		for _, src := range s.Sources {
			k := [2]string{s.Account, src}
			l.sourcedInto[k] = append(l.sourcedInto[k], s.Stream)
		}
	}
	for _, m := range []map[[2]string][]string{l.mirroredBy, l.sourcedInto} {
		for k := range m {
			slices.Sort(m[k])
			m[k] = slices.Compact(m[k])
		}
	}
	return l
}

// FinishSubjects builds the response from the facts for the request's
// account scope and filter: one row per stream, per subscription group,
// per account's inbox fold and per account's JetStream API; the notes'
// facts, the counts, the order and the cut at MaxSubjectRows.
func FinishSubjects(f *SubjectsFacts, account string, filter v1.SubjectFilter) *v1.GetSubjectsResponse {
	if f == nil || f.Epoch == 0 {
		return &v1.GetSubjectsResponse{}
	}
	res := &v1.GetSubjectsResponse{Epoch: f.Epoch, EpochAt: timestamppb.New(f.EpochAt)}
	var streams []SubjectStreamFact
	for _, s := range f.Streams {
		if inScope(account, s.Account) {
			streams = append(streams, s)
		}
	}
	links := linksOf(streams)
	deliver := map[[2]string]SubscriptionGroup{}
	if sf := f.Subscriptions; sf != nil {
		res.SubscriptionsAt = timestamppb.New(sf.At)
		for _, g := range sf.Deliver {
			deliver[[2]string{g.Account, g.Subject}] = g
		}
		for _, sv := range sf.Servers {
			res.SubszServers = append(res.SubszServers, &v1.SubszServer{ServerName: sv.Name, Answered: sv.Answered, Total: sv.Total,
				Listed: sv.Listed})
		}
		slices.SortFunc(res.SubszServers, func(a, b *v1.SubszServer) int { return cmp.Compare(a.GetServerName(), b.GetServerName()) })
		for acc, n := range sf.ServerSubscriptions {
			if inScope(account, acc) {
				res.ServerSubscriptions += n
			}
		}
		for acc, n := range sf.Unlisted {
			if inScope(account, acc) {
				res.UnlistedConnectionSubscriptions += n
			}
		}
	}
	var rows []*v1.SubjectRow
	for _, s := range streams {
		rows = append(rows, streamRow(s, streams, links, deliver))
	}
	if sf := f.Subscriptions; sf != nil {
		for _, g := range sf.Groups {
			if !inScope(account, g.Account) {
				continue
			}
			r := &v1.SubjectRow{Account: g.Account, Kind: v1.SubjectRowKind_SUBJECT_ROW_KIND_SUBSCRIPTION, Patterns: []string{g.Subject},
				Connections: g.Connections, Queue: g.Queue}
			if g.Subject == InboxPattern {
				r.Kind = v1.SubjectRowKind_SUBJECT_ROW_KIND_INBOX
			} else {
				r.Wide = g.Subject == ">" || strings.HasSuffix(g.Subject, ".>")
			}
			r.ClientNames, r.MoreNames = cutNames(g.Names, g.NameCount)
			rows = append(rows, r)
		}
	}
	for _, a := range f.APIs {
		if inScope(account, a.Account) {
			rows = append(rows, &v1.SubjectRow{Account: a.Account, Kind: v1.SubjectRowKind_SUBJECT_ROW_KIND_JETSTREAM_API,
				Patterns: []string{JetStreamAPIPattern}, ApiPerSecond: a.PerSecond, ApiErrorShare: a.ErrorShare})
		}
	}
	for _, r := range rows {
		r.SystemAccount = f.SystemAccount != "" && r.GetAccount() == f.SystemAccount
		res.AllCount++
		if storedKind(r.GetKind()) {
			res.StoredCount++
		}
		if r.GetNoConsumer() {
			res.NoConsumerCount++
		}
		if len(r.GetOverlaps()) > 0 {
			res.OverlappingCount++
		}
		if r.GetWide() {
			res.WideCount++
		}
	}
	rows = slices.DeleteFunc(rows, func(r *v1.SubjectRow) bool { return !SubjectFilterKeeps(filter, r) })
	slices.SortStableFunc(rows, compareSubjectRows)
	res.RowsTotal = int32(len(rows))
	res.Rows = rows[:min(len(rows), MaxSubjectRows)]
	return res
}

// streamRow is a stream's row: its subjects, consumers, the names of its
// push consumers' subscribers, links and the notes' facts.
func streamRow(s SubjectStreamFact, all []SubjectStreamFact, links streamLinks, deliver map[[2]string]SubscriptionGroup) *v1.SubjectRow {
	key := [2]string{s.Account, s.Stream}
	r := &v1.SubjectRow{Account: s.Account, Kind: SubjectKind(s.Stream), Stream: s.Stream, MirrorOf: s.Mirror, Retention: s.Retention,
		Messages: s.Messages, StoredPerSecond: s.StoredPerSecond, MirroredBy: links.mirroredBy[key], SourcedInto: links.sourcedInto[key]}
	if s.Mirror == "" {
		r.Patterns = slices.Clone(s.Subjects)
	}
	var names []string
	for _, c := range s.Consumers {
		if c.Durable {
			r.DurableConsumers++
		} else {
			r.EphemeralConsumers++
		}
		if c.DeliverSubject != "" {
			names = append(names, deliver[[2]string{s.Account, c.DeliverSubject}].Names...)
		}
	}
	r.ClientNames, r.MoreNames = cutNames(names, 0)
	for _, o := range all {
		if o.Account != s.Account || o.Stream == s.Stream || o.Mirror != "" || s.Mirror != "" {
			continue
		}
		if overlapping(s.Subjects, o.Subjects) {
			r.Overlaps = append(r.Overlaps, o.Stream)
		}
	}
	slices.Sort(r.Overlaps)
	// A mirror kept as a copy is read by none on purpose.
	r.NoConsumer = r.Kind == v1.SubjectRowKind_SUBJECT_ROW_KIND_STREAM && s.Mirror == "" && len(s.Consumers) == 0 &&
		len(r.MirroredBy) == 0 && len(r.SourcedInto) == 0
	return r
}

// overlapping reports whether a subject of a overlaps one of b.
func overlapping(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if SubjectsOverlap(x, y) {
				return true
			}
		}
	}
	return false
}

// SubjectFilterKeeps reports whether a filter keeps a row.
func SubjectFilterKeeps(filter v1.SubjectFilter, r *v1.SubjectRow) bool {
	switch filter {
	case v1.SubjectFilter_SUBJECT_FILTER_STORED:
		return storedKind(r.GetKind())
	case v1.SubjectFilter_SUBJECT_FILTER_NO_CONSUMER:
		return r.GetNoConsumer()
	case v1.SubjectFilter_SUBJECT_FILTER_OVERLAPPING:
		return len(r.GetOverlaps()) > 0
	case v1.SubjectFilter_SUBJECT_FILTER_WIDE:
		return r.GetWide()
	}
	return true
}

// SubjectRowWarns reports whether a row's note is a warning: an overlap
// or a stream without a consumer.
func SubjectRowWarns(r *v1.SubjectRow) bool { return len(r.GetOverlaps()) > 0 || r.GetNoConsumer() }

// rowPattern is the pattern a row orders by: its first, else its stream.
func rowPattern(r *v1.SubjectRow) string {
	if len(r.GetPatterns()) > 0 {
		return r.GetPatterns()[0]
	}
	return r.GetStream()
}

// compareSubjectRows puts the rows that warn first; then by account, the
// system account last; within an account, stored rows before the others;
// stored rows by stored rate, most first (unset last), the others by
// connections, most first; then by pattern and stream.
func compareSubjectRows(a, b *v1.SubjectRow) int {
	rate := func(r *v1.SubjectRow) *float64 {
		if r.StoredPerSecond == nil {
			return nil
		}
		v := -r.GetStoredPerSecond()
		return &v
	}
	return cmp.Or(compareBool(!SubjectRowWarns(a), !SubjectRowWarns(b)), compareBool(a.GetSystemAccount(), b.GetSystemAccount()),
		cmp.Compare(a.GetAccount(), b.GetAccount()), compareBool(!storedKind(a.GetKind()), !storedKind(b.GetKind())),
		compareSoonest(rate(a), rate(b)), cmp.Compare(b.GetConnections(), a.GetConnections()),
		cmp.Compare(rowPattern(a), rowPattern(b)), cmp.Compare(a.GetStream(), b.GetStream()))
}

// MatchFacts are what a provider read for one MatchSubject: the epoch's
// streams as for GetSubjects, and the subscription collection's groups by
// account, subject and queue (CoreGroups), at least those whose subject
// matches. FinishMatch keeps what matches.
type MatchFacts struct {
	Epoch   int64
	EpochAt time.Time
	Streams []SubjectStreamFact
	// SubscriptionsAt is nil without a subscription collection.
	SubscriptionsAt *time.Time
	Core            []SubscriptionGroup
}

// FinishMatch builds the tester's answer for a literal subject in the
// account scope: each stream whose subject matches, with the first of its
// subjects that does, its consumers whose filter matches, and the streams
// that mirror or source it; and the subscription groups whose subject
// matches.
func FinishMatch(f *MatchFacts, account, subject string) *v1.MatchSubjectResponse {
	if f == nil || f.Epoch == 0 {
		return &v1.MatchSubjectResponse{}
	}
	res := &v1.MatchSubjectResponse{Epoch: f.Epoch, EpochAt: timestamppb.New(f.EpochAt)}
	var streams []SubjectStreamFact
	for _, s := range f.Streams {
		if inScope(account, s.Account) {
			streams = append(streams, s)
		}
	}
	links := linksOf(streams)
	for _, s := range streams {
		if s.Mirror != "" {
			continue
		}
		i := slices.IndexFunc(s.Subjects, func(p string) bool { return SubjectMatches(subject, p) })
		if i < 0 {
			continue
		}
		key := [2]string{s.Account, s.Stream}
		m := &v1.SubjectMatch{Account: s.Account, Stream: s.Stream, Pattern: s.Subjects[i], Mirrors: links.mirroredBy[key],
			SourcedBy: links.sourcedInto[key]}
		for _, c := range s.Consumers {
			if len(c.Filters) > 0 && !slices.ContainsFunc(c.Filters, func(p string) bool { return SubjectMatches(subject, p) }) {
				continue
			}
			if c.Durable {
				m.Consumers = append(m.Consumers, c.Name)
			} else {
				m.EphemeralConsumers++
			}
		}
		slices.Sort(m.Consumers)
		res.Matches = append(res.Matches, m)
	}
	slices.SortFunc(res.Matches, func(a, b *v1.SubjectMatch) int {
		return cmp.Or(cmp.Compare(a.GetAccount(), b.GetAccount()), cmp.Compare(a.GetStream(), b.GetStream()))
	})
	if f.SubscriptionsAt != nil {
		res.SubscriptionsAt = timestamppb.New(*f.SubscriptionsAt)
		core := slices.Clone(f.Core)
		sortGroups(core)
		for _, g := range core {
			if !inScope(account, g.Account) || !SubjectMatches(subject, g.Subject) {
				continue
			}
			c := &v1.CoreMatch{Account: g.Account, Subject: g.Subject, Queue: g.Queue, Connections: g.Connections}
			c.ClientNames, c.MoreNames = cutNames(g.Names, g.NameCount)
			res.Subscribers = append(res.Subscribers, c)
		}
	}
	return res
}
