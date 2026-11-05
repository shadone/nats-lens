package apitest

import (
	"fmt"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// acknowledgedWith finds a firing finding whose acknowledgement in force
// is quiet, or not.
func acknowledgedWith(quiet bool) func(Q) string {
	return func(q Q) string {
		for _, f := range q.findings() {
			if a := f.GetAcknowledgement(); a != nil && a.GetQuiet() == quiet && a.GetEndedAt() == nil && a.GetAckedBy() != "" {
				return f.GetId() + " by " + a.GetAckedBy()
			}
		}
		return ""
	}
}

// oncallRows are the states of acknowledgements.
func oncallRows() []State {
	return []State{
		{"a finding acknowledged, its quiet in force", acknowledgedWith(true)},
		{"a finding acknowledged, its quiet over", acknowledgedWith(false)},
		{"the tally counts the acknowledged findings", func(q Q) string {
			n := 0
			for _, f := range q.findings() {
				if f.GetAcknowledgement() != nil {
					n++
				}
			}
			tally := must(q.env.Client.GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{System: q.System, Range: q.day()}))).of(q.t).GetTally()
			if n == 0 || int(tally.GetAcknowledged()) != n {
				return ""
			}
			return fmt.Sprintf("%d acknowledged", n)
		}},
		// It writes: an acknowledgement of a finding that had none, handed
		// back at once.
		{"an acknowledgement handed back does not show on its finding", func(q Q) string {
			for _, f := range q.findings() {
				if f.GetAcknowledgement() != nil {
					continue
				}
				a := must(q.env.Client.AcknowledgeFinding(ctx, connect.NewRequest(&v1.AcknowledgeFindingRequest{System: q.System,
					FindingId: f.GetId(), QuietFor: durationpb.New(api.MinAckQuiet), AckedBy: "conformance"}))).of(q.t).GetAcknowledgement()
				end := must(q.env.Client.EndAcknowledgement(ctx, connect.NewRequest(&v1.EndAcknowledgementRequest{System: q.System,
					Id: a.GetId(), EndedBy: "conformance"}))).of(q.t).GetAcknowledgement()
				got := must(q.env.Client.GetFinding(ctx, connect.NewRequest(&v1.GetFindingRequest{System: q.System, Id: f.GetId()}))).of(q.t).GetFinding()
				if end.GetEndReason() != "released" || got.GetAcknowledgement() != nil {
					q.t.Errorf("%s: ended %v, finding's acknowledgement %v", f.GetId(), end, got.GetAcknowledgement())
					return ""
				}
				return f.GetId()
			}
			return ""
		}},
		{"an acknowledgement of a resolved finding is refused", func(q Q) string {
			for _, f := range q.findings(v1.FindingState_FINDING_STATE_RESOLVED) {
				_, err := q.env.Client.AcknowledgeFinding(ctx, connect.NewRequest(&v1.AcknowledgeFindingRequest{System: q.System,
					FindingId: f.GetId(), UntilResolved: true, AckedBy: "conformance"}))
				if connect.CodeOf(err) == connect.CodeFailedPrecondition {
					return f.GetId()
				}
			}
			return ""
		}},
	}
}
