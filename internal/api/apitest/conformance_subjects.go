package apitest

import (
	"fmt"
	"strings"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// subjects reads the Subjects page of the system's last day.
func (q Q) subjects(account string, filter v1.SubjectFilter) *v1.GetSubjectsResponse {
	q.t.Helper()
	return must(q.env.Client.GetSubjects(ctx, connect.NewRequest(&v1.GetSubjectsRequest{System: q.System, Range: q.day(), Account: account,
		Filter: filter}))).of(q.t)
}

// match asks the tester of the system's last day.
func (q Q) match(account, subject string) *v1.MatchSubjectResponse {
	q.t.Helper()
	return must(q.env.Client.MatchSubject(ctx, connect.NewRequest(&v1.MatchSubjectRequest{System: q.System, Range: q.day(), Account: account,
		Subject: subject}))).of(q.t)
}

// subjectRowKey names a row for the log.
func subjectRowKey(r *v1.SubjectRow) string {
	if r.GetStream() != "" {
		return r.GetAccount() + "/" + r.GetStream()
	}
	return r.GetAccount() + " " + strings.Join(r.GetPatterns(), ",")
}

// subjectRow finds a row of the page that ok accepts.
func subjectRow(ok func(*v1.SubjectRow) bool) func(Q) string {
	return func(q Q) string {
		for _, r := range q.subjects("", 0).GetRows() {
			if ok(r) {
				return subjectRowKey(r)
			}
		}
		return ""
	}
}

// literalOf is a subject a pattern matches: each wildcard token replaced.
func literalOf(pattern string) string {
	toks := strings.Split(pattern, ".")
	for i, t := range toks {
		if t == "*" || t == ">" {
			toks[i] = "conformance"
		}
	}
	return strings.Join(toks, ".")
}

func storedRow(r *v1.SubjectRow) bool {
	switch r.GetKind() {
	case v1.SubjectRowKind_SUBJECT_ROW_KIND_STREAM, v1.SubjectRowKind_SUBJECT_ROW_KIND_KEY_VALUE, v1.SubjectRowKind_SUBJECT_ROW_KIND_OBJECT_STORE:
		return true
	}
	return false
}

// subjectsRows are the Subjects states both providers must show.
func subjectsRows() []State {
	return []State{
		{"a Subjects stream row with its durable consumers and stored rate", subjectRow(func(r *v1.SubjectRow) bool {
			return r.GetKind() == v1.SubjectRowKind_SUBJECT_ROW_KIND_STREAM && r.GetDurableConsumers() > 0 && r.StoredPerSecond != nil &&
				len(r.GetPatterns()) > 0 && r.Messages != nil
		})},
		{"a Subjects stream row whose push consumer's connection is named, its deliver subject no row of its own", subjectRow(func(r *v1.SubjectRow) bool {
			return storedRow(r) && r.GetEphemeralConsumers() > 0 && len(r.GetClientNames()) > 0
		})},
		{"a Subjects stream row without a consumer", subjectRow(func(r *v1.SubjectRow) bool {
			return r.GetNoConsumer() && r.GetDurableConsumers() == 0 && r.GetEphemeralConsumers() == 0 && r.GetMirrorOf() == "" &&
				len(r.GetMirroredBy()) == 0 && len(r.GetSourcedInto()) == 0
		})},
		{"a Subjects row of a mirror, without patterns", subjectRow(func(r *v1.SubjectRow) bool {
			return r.GetMirrorOf() != "" && len(r.GetPatterns()) == 0 && !r.GetNoConsumer()
		})},
		{"a Subjects row of a KV bucket", subjectRow(func(r *v1.SubjectRow) bool {
			return r.GetKind() == v1.SubjectRowKind_SUBJECT_ROW_KIND_KEY_VALUE && len(r.GetPatterns()) == 1 && strings.HasPrefix(r.GetPatterns()[0], "$KV.")
		})},
		{"a Subjects subscription row in a queue group", subjectRow(func(r *v1.SubjectRow) bool {
			return r.GetKind() == v1.SubjectRowKind_SUBJECT_ROW_KIND_SUBSCRIPTION && r.GetQueue() != "" && r.GetConnections() > 1 &&
				len(r.GetClientNames()) > 0
		})},
		{"a Subjects wide subscription row", subjectRow(func(r *v1.SubjectRow) bool {
			return r.GetKind() == v1.SubjectRowKind_SUBJECT_ROW_KIND_SUBSCRIPTION && r.GetWide() && strings.HasSuffix(r.GetPatterns()[0], ">")
		})},
		{"a Subjects JetStream API row with its rate", subjectRow(func(r *v1.SubjectRow) bool {
			return r.GetKind() == v1.SubjectRowKind_SUBJECT_ROW_KIND_JETSTREAM_API && r.ApiPerSecond != nil && r.ApiErrorShare != nil &&
				r.GetPatterns()[0] == api.JetStreamAPIPattern
		})},
		{"a Subjects page whose inboxes fold into one row per account", func(q Q) string {
			res := q.subjects("", 0)
			n := map[string]int{}
			for _, r := range res.GetRows() {
				for _, p := range r.GetPatterns() {
					if strings.HasPrefix(p, api.InboxPrefix) && (p != api.InboxPattern || r.GetKind() != v1.SubjectRowKind_SUBJECT_ROW_KIND_INBOX) {
						q.t.Errorf("%s: row %s holds inbox %s", q.System, subjectRowKey(r), p)
						return ""
					}
				}
				if r.GetKind() == v1.SubjectRowKind_SUBJECT_ROW_KIND_INBOX {
					n[r.GetAccount()]++
				}
			}
			for acc, c := range n {
				if c != 1 {
					q.t.Errorf("%s: %d inbox rows in %s", q.System, c, acc)
					return ""
				}
			}
			if len(n) == 0 {
				return ""
			}
			return fmt.Sprintf("%d accounts", len(n))
		}},
		{"a Subjects page that leaves the servers' own subscriptions out and counts them", func(q Q) string {
			res := q.subjects("", 0)
			if res.SubscriptionsAt == nil || res.GetServerSubscriptions() == 0 {
				return ""
			}
			return fmt.Sprintf("%d", res.GetServerSubscriptions())
		}},
		{"a Subjects page with a server whose list was cut and one that did not answer", func(q Q) string {
			var cut, silent string
			for _, s := range q.subjects("", 0).GetSubszServers() {
				switch {
				case !s.GetAnswered() && s.Total == nil && s.Listed == nil:
					silent = s.GetServerName()
				case s.GetAnswered() && s.GetListed() < s.GetTotal():
					cut = s.GetServerName()
				}
			}
			if cut == "" || silent == "" {
				return ""
			}
			return "cut " + cut + ", silent " + silent
		}},
		{"a Subjects page without a subscription collection", func(q Q) string {
			res := q.subjects("", 0)
			if res.GetEpoch() == 0 || res.SubscriptionsAt != nil {
				return ""
			}
			for _, r := range res.GetRows() {
				if r.GetConnections() > 0 || len(r.GetClientNames()) > 0 {
					q.t.Errorf("%s: row %s without a collection", q.System, subjectRowKey(r))
				}
			}
			if len(res.GetSubszServers()) != 0 || res.GetServerSubscriptions() != 0 {
				q.t.Errorf("%s: servers %v without a collection", q.System, res.GetSubszServers())
			}
			return fmt.Sprintf("%d rows", len(res.GetRows()))
		}},
		{"the Subjects counts are the rows' and each filter's rows, in every system", everySystem(func(q Q) string {
			res := q.subjects("", 0)
			if res.GetRowsTotal() > api.MaxSubjectRows || int(res.GetRowsTotal()) != len(res.GetRows()) || res.GetAllCount() != res.GetRowsTotal() {
				q.t.Errorf("%s: %d rows, total %d, all %d", q.System, len(res.GetRows()), res.GetRowsTotal(), res.GetAllCount())
				return ""
			}
			want := map[v1.SubjectFilter]int32{
				v1.SubjectFilter_SUBJECT_FILTER_STORED: res.GetStoredCount(), v1.SubjectFilter_SUBJECT_FILTER_NO_CONSUMER: res.GetNoConsumerCount(),
				v1.SubjectFilter_SUBJECT_FILTER_OVERLAPPING: res.GetOverlappingCount(), v1.SubjectFilter_SUBJECT_FILTER_WIDE: res.GetWideCount(),
			}
			for f, n := range want {
				got := q.subjects("", f)
				if got.GetRowsTotal() != n || got.GetAllCount() != res.GetAllCount() {
					q.t.Errorf("%s %v: %d rows, the count says %d", q.System, f, got.GetRowsTotal(), n)
					return ""
				}
				for _, r := range got.GetRows() {
					if !api.SubjectFilterKeeps(f, r) {
						q.t.Errorf("%s %v: row %s", q.System, f, subjectRowKey(r))
					}
				}
			}
			// Overlap is symmetric, within an account.
			byKey := map[string]*v1.SubjectRow{}
			for _, r := range res.GetRows() {
				byKey[subjectRowKey(r)] = r
			}
			for _, r := range res.GetRows() {
				for _, o := range r.GetOverlaps() {
					if other := byKey[r.GetAccount()+"/"+o]; other == nil || !containsString(other.GetOverlaps(), r.GetStream()) {
						q.t.Errorf("%s: %s overlaps %s, not the other way", q.System, subjectRowKey(r), o)
					}
				}
			}
			if res.GetEpoch() == 0 {
				return ""
			}
			return fmt.Sprintf("%d rows, %d overlapping", res.GetAllCount(), res.GetOverlappingCount())
		})},
		{"the Subjects stream rows are the streams ListStreams lists, with their consumers and messages, in every system", everySystem(func(q Q) string {
			res := q.subjects("", 0)
			list := map[string]*v1.StreamSummary{}
			for _, s := range q.streams(q.day()) {
				list[s.GetEntity().GetKey()] = s
			}
			n := 0
			for _, r := range res.GetRows() {
				if !storedRow(r) {
					continue
				}
				n++
				l := list[r.GetAccount()+"/"+r.GetStream()]
				if l == nil || l.GetConsumers() != r.GetDurableConsumers()+r.GetEphemeralConsumers() || (l.Messages == nil) != (r.Messages == nil) ||
					l.GetMessages() != r.GetMessages() {
					q.t.Errorf("%s: row %v, ListStreams %v", q.System, r, l)
					return ""
				}
			}
			if n != len(list) {
				q.t.Errorf("%s: %d stream rows, %d streams", q.System, n, len(list))
				return ""
			}
			if n == 0 {
				return ""
			}
			return fmt.Sprintf("%d streams", n)
		})},
		{"the Subjects tester names a stream with its consumers, the pattern that matched and its mirror", func(q Q) string {
			for _, r := range q.subjects("", 0).GetRows() {
				if r.GetKind() != v1.SubjectRowKind_SUBJECT_ROW_KIND_STREAM || r.GetDurableConsumers() == 0 || len(r.GetMirroredBy()) == 0 {
					continue
				}
				res := q.match(r.GetAccount(), literalOf(r.GetPatterns()[0]))
				for _, m := range api.MatchedStreams(res) {
					if m.GetStream() == r.GetStream() && m.GetPattern() == r.GetPatterns()[0] && len(m.GetConsumers()) > 0 &&
						containsString(m.GetMirrors(), r.GetMirroredBy()[0]) {
						return r.GetStream()
					}
				}
				q.t.Errorf("%s: %s not matched: %v", q.System, r.GetStream(), api.MatchedStreams(res))
				return ""
			}
			return ""
		}},
		{"the Subjects tester names a stream that sources the matched one", func(q Q) string {
			for _, r := range q.subjects("", 0).GetRows() {
				if r.GetKind() != v1.SubjectRowKind_SUBJECT_ROW_KIND_STREAM || len(r.GetSourcedInto()) == 0 || len(r.GetPatterns()) == 0 {
					continue
				}
				for _, m := range api.MatchedStreams(q.match(r.GetAccount(), literalOf(r.GetPatterns()[0]))) {
					if m.GetStream() == r.GetStream() && containsString(m.GetSourcedBy(), r.GetSourcedInto()[0]) {
						return r.GetStream()
					}
				}
			}
			return ""
		}},
		{"the Subjects tester counts a queue group once, with its connections", func(q Q) string {
			for _, r := range q.subjects("", 0).GetRows() {
				if r.GetKind() != v1.SubjectRowKind_SUBJECT_ROW_KIND_SUBSCRIPTION || r.GetQueue() == "" || r.GetConnections() < 2 ||
					literalOf(r.GetPatterns()[0]) != r.GetPatterns()[0] {
					continue
				}
				for _, c := range api.MatchedSubscribers(q.match(r.GetAccount(), r.GetPatterns()[0])) {
					if c.GetSubject() == r.GetPatterns()[0] && c.GetQueue() == r.GetQueue() && c.GetConnections() == r.GetConnections() {
						return r.GetPatterns()[0]
					}
				}
			}
			return ""
		}},
		{"the Subjects tester finds nothing for a subject nobody uses", func(q Q) string {
			for _, r := range q.subjects("", 0).GetRows() {
				if storedRow(r) {
					res := q.match(r.GetAccount(), "conformance.nobody.uses.this")
					if len(api.MatchedStreams(res)) != 0 || len(api.MatchedSubscribers(res)) != 0 || res.GetEpoch() == 0 {
						return ""
					}
					return r.GetAccount()
				}
			}
			return ""
		}},
		{"the Subjects tester leaves the system account out of an answer without a scope, but for a $SYS. subject", func(q Q) string {
			for _, r := range q.subjects("", 0).GetRows() {
				if !r.GetSystemAccount() || r.GetKind() != v1.SubjectRowKind_SUBJECT_ROW_KIND_SUBSCRIPTION || r.GetPatterns()[0] != ">" {
					continue
				}
				for _, a := range q.match("", "conformance.application.subject").GetAccounts() {
					if a.GetSystemAccount() || a.GetAccount() == r.GetAccount() {
						q.t.Errorf("%s: an application subject reaches %s", q.System, a.GetAccount())
						return ""
					}
				}
				for _, a := range q.match("", "$SYS.conformance").GetAccounts() {
					if a.GetAccount() == r.GetAccount() && a.GetSystemAccount() {
						for _, c := range a.GetSubscribers() {
							if c.GetSubject() == ">" {
								return r.GetAccount()
							}
						}
					}
				}
				q.t.Errorf("%s: $SYS.conformance does not reach %s's >", q.System, r.GetAccount())
				return ""
			}
			return ""
		}},
		{"the Subjects tester answers a $JS.API. subject with the account's JetStream API", func(q Q) string {
			for _, r := range q.subjects("", 0).GetRows() {
				if r.GetKind() != v1.SubjectRowKind_SUBJECT_ROW_KIND_JETSTREAM_API {
					continue
				}
				res := q.match(r.GetAccount(), "$JS.API.INFO")
				if len(res.GetAccounts()) != 1 || !res.GetAccounts()[0].GetJetstreamApi() {
					q.t.Errorf("%s: $JS.API.INFO in %s: %v", q.System, r.GetAccount(), res)
					return ""
				}
				return r.GetAccount()
			}
			return ""
		}},
		{"the Subjects tester groups an answer without a scope by account, each with something that matches, in every system", everySystem(func(q Q) string {
			n := 0
			for _, r := range q.subjects("", 0).GetRows() {
				if r.GetKind() != v1.SubjectRowKind_SUBJECT_ROW_KIND_STREAM || len(r.GetPatterns()) == 0 {
					continue
				}
				res := q.match("", literalOf(r.GetPatterns()[0]))
				seen := map[string]bool{}
				for i, a := range res.GetAccounts() {
					if seen[a.GetAccount()] || len(a.GetMatches())+len(a.GetSubscribers()) == 0 && !a.GetJetstreamApi() {
						q.t.Errorf("%s: group %v", q.System, a)
						return ""
					}
					seen[a.GetAccount()] = true
					for _, m := range a.GetMatches() {
						if m.GetAccount() != a.GetAccount() {
							q.t.Errorf("%s: %s's group holds %s", q.System, a.GetAccount(), m.GetAccount())
						}
					}
					if i > 0 && res.GetAccounts()[i-1].GetSystemAccount() {
						q.t.Errorf("%s: the system account is not last", q.System)
					}
				}
				if !seen[r.GetAccount()] {
					q.t.Errorf("%s: %s has no group for its own stream %s", q.System, r.GetAccount(), r.GetStream())
					return ""
				}
				n = max(n, len(res.GetAccounts()))
			}
			if n == 0 {
				return ""
			}
			return fmt.Sprintf("up to %d accounts", n)
		})},
		{"the Subjects tester refuses a subject that is not a literal, in every system", everySystem(func(q Q) string {
			for _, s := range []string{"orders.*", "orders.>", "orders..x", "", "orders\x00x"} {
				_, err := q.env.Client.MatchSubject(ctx, connect.NewRequest(&v1.MatchSubjectRequest{System: q.System, Subject: s}))
				if connect.CodeOf(err) != connect.CodeInvalidArgument {
					q.t.Errorf("%s %q: %v, want invalid_argument", q.System, s, err)
					return ""
				}
			}
			return "refused"
		})},
	}
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
