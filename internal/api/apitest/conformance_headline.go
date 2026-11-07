package apitest

import (
	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"

	"github.com/shadone/nats-lens/internal/checks"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// headlineRows are the states of ranking and naming both providers must
// show: an expiry warning marked expiry and ranked after a warning that
// is not, though it fires longer; a mute of an account finding named by
// the account's name; and the systems list's worst finding as the
// Findings list carries it, named alike.
func headlineRows() []State {
	return []State{
		{"an expiry warning, marked expiry, ranked after a newer warning that is not an expiry", func(q Q) string {
			fs := q.findings()
			for _, f := range fs {
				if f.GetExpiry() != checks.IsExpiry(f.GetCheckCode()) {
					q.t.Errorf("%s: expiry %v, its check's category says %v", f.GetId(), f.GetExpiry(), !f.GetExpiry())
				}
			}
			live := func(f *v1.Finding) bool {
				return f.GetState() != v1.FindingState_FINDING_STATE_RESOLVED && f.GetMute() == nil &&
					f.GetSeverity() == v1.Severity_SEVERITY_WARNING
			}
			for i, n := range fs {
				if !live(n) || n.GetExpiry() {
					continue
				}
				for _, e := range fs[i+1:] {
					if live(e) && e.GetExpiry() && e.GetSince().AsTime().Before(n.GetSince().AsTime()) {
						return n.GetId() + " before " + e.GetId()
					}
				}
			}
			return ""
		}},
		{"a mute of an account finding named by the account's name", func(q Q) string {
			ms := must(q.env.Client.ListMutes(ctx, connect.NewRequest(&v1.ListMutesRequest{System: q.System, IncludeInactive: true}))).of(q.t).GetMutes()
			for _, m := range ms {
				e := m.GetEntity()
				if e.GetType() == v1.EntityType_ENTITY_TYPE_ACCOUNT && e.GetAccount() == e.GetKey() && e.GetAccountName() != "" {
					return m.GetCheckCode() + " " + e.GetAccountName()
				}
			}
			return ""
		}},
		{"the systems list's worst finding as the Findings list carries it", func(q Q) string {
			w := q.summary().GetWorstFinding()
			if w == nil {
				return ""
			}
			for _, f := range q.findings() {
				if f.GetId() == w.GetId() {
					if !proto.Equal(f.GetEntity(), w.GetEntity()) || f.GetExpiry() != w.GetExpiry() {
						q.t.Errorf("%s: systems list entity %v expiry %v, Findings list %v expiry %v", f.GetId(), w.GetEntity(), w.GetExpiry(),
							f.GetEntity(), f.GetExpiry())
					}
					return w.GetId()
				}
			}
			return ""
		}},
	}
}
