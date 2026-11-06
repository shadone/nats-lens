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

// cardRows are shared observations of the real store and the demo.
func cardRows() []State {
	card := func(test func(*v1.GetOnCallResponse) bool) func(Q) string {
		return func(q Q) string {
			fs := append(q.findings(), q.findings(v1.FindingState_FINDING_STATE_RESOLVED)...)
			for _, f := range fs {
				got := must(q.env.Client.GetOnCall(ctx, connect.NewRequest(&v1.GetOnCallRequest{System: q.System, FindingId: f.Id}))).of(q.t)
				if got.GetFinding().GetId() != f.Id || len(got.Assets) > 10 || len(got.Steps) > 12 {
					q.t.Fatalf("invalid card: %v", got)
				}
				if test(got) {
					return f.Id
				}
			}
			return ""
		}
	}
	return []State{
		{"an on-call card with a context window", card(func(c *v1.GetOnCallResponse) bool { return c.ContextWindow != nil })},
		{"an on-call card without a context window", card(func(c *v1.GetOnCallResponse) bool { return c.ContextWindow == nil })},
		{"an on-call asset with observed replicas online", card(func(c *v1.GetOnCallResponse) bool {
			for _, a := range c.Assets {
				if a.Replicas != nil && a.Online != nil {
					return true
				}
			}
			return false
		})},
		{"an on-call asset whose online count was not reported", card(func(c *v1.GetOnCallResponse) bool {
			for _, a := range c.Assets {
				if a.Online == nil {
					return true
				}
			}
			return false
		})},
		{"an on-call story cut to twelve steps with its total", card(func(c *v1.GetOnCallResponse) bool { return len(c.Steps) == 12 && c.StepsTotal > 12 })},
		{"a resolved on-call finding ends its story at resolve", card(func(c *v1.GetOnCallResponse) bool {
			return c.Finding.State == v1.FindingState_FINDING_STATE_RESOLVED && c.Finding.ResolvedAt.AsTime().Equal(c.StoryB.AsTime()) && c.Finding.Acknowledgement == nil
		})},
	}
}
