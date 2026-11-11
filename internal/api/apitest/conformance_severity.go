package apitest

import (
	"fmt"
	"strings"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/checks"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// severityRows are the three ways a finding says why it has its severity:
// its check has no severity rule; it has one and the finding is at the
// usual severity; the rule raised or lowered it. Every finding's reason,
// in the list and on its own page, is the one its check's header gives.
func severityRows() []State {
	word := func(f *v1.Finding) string {
		return strings.TrimPrefix(strings.ToLower(f.GetSeverity().String()), "severity_")
	}
	reasonIs := func(want func(f *v1.Finding, usual, rule string) bool) func(Q) string {
		return func(q Q) string {
			found := ""
			for _, f := range q.findings() {
				usual, rule, ok := checks.UsualSeverity(f.GetCheckCode())
				if !ok {
					continue
				}
				expect := api.SeverityReason(f.GetCheckCode(), f.GetSeverity(), f.GetState() == v1.FindingState_FINDING_STATE_RESOLVED, usual, rule)
				if f.GetSeverityReason() != expect {
					q.t.Errorf("%s: severity reason %q, want %q", f.GetId(), f.GetSeverityReason(), expect)
					return ""
				}
				if rule == "" && word(f) != usual {
					q.t.Errorf("%s: %s, but its check is always %s and has no severity rule", f.GetId(), word(f), usual)
					return ""
				}
				if found != "" || !want(f, usual, rule) {
					continue
				}
				one := must(q.env.Client.GetFinding(ctx, connect.NewRequest(&v1.GetFindingRequest{System: q.System, Id: f.GetId()}))).of(q.t).GetFinding()
				if one.GetSeverityReason() != expect {
					q.t.Errorf("%s: GetFinding's severity reason %q, the list's %q", f.GetId(), one.GetSeverityReason(), expect)
					return ""
				}
				found = f.GetId() + ": " + expect
			}
			return found
		}
	}
	return []State{
		{"a finding of a check without a severity rule says its severity does not change", reasonIs(func(f *v1.Finding, usual, rule string) bool {
			return rule == "" && word(f) == usual
		})},
		{"a finding at its check's usual severity gives the rule for another", reasonIs(func(f *v1.Finding, usual, rule string) bool {
			return rule != "" && word(f) == usual
		})},
		{"a finding its check's severity rule raised or lowered says so", reasonIs(func(f *v1.Finding, usual, rule string) bool {
			return rule != "" && word(f) != usual
		})},
		// An occurrence is read with the check definition unavailable
		// (historical_check_definition), so it gives no reason: today's
		// header would misdescribe a past severity.
		{"a finding occurrence gives no severity reason, in every system", everySystem(func(q Q) string {
			res := must(q.env.Client.ListFindingEpisodes(ctx, connect.NewRequest(&v1.ListFindingEpisodesRequest{System: q.System, Range: q.day(), PageSize: 100}))).of(q.t)
			for _, e := range res.GetEpisodes() {
				if r := e.GetFinding().GetSeverityReason(); r != "" {
					q.t.Errorf("%s occurrence at %d: severity reason %q", e.GetFinding().GetId(), e.GetFirstRetainedEpoch(), r)
					return ""
				}
			}
			if len(res.GetEpisodes()) == 0 {
				return ""
			}
			return fmt.Sprintf("%d occurrences", len(res.GetEpisodes()))
		})},
	}
}
