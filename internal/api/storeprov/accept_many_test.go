package storeprov_test

import (
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// Accepting several entities at once stores one acceptance each; the
// review list shows them soonest first, follows the account, and the
// tally counts the accepted finding apart within muted.
func TestAcceptFindingsAndReviewList(t *testing.T) {
	st, set, _ := findingsStore(t)
	_, c := apitest.Serve(t, &storeprov.Provider{St: st, Interval: 30 * time.Second, Live: true, Checks: set, Clock: func() time.Time { return now }})
	stream := func(key string) *v1.EntityRef { return &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_STREAM, Key: key} }
	day := 24 * time.Hour
	for _, f := range [][3]string{{"stream-005", "OTHER/archive", "firing"}, {"stream-018", account + "/single", "firing"}, {"stream-005", account + "/gone", "resolved"}} {
		if _, err := st.DB().Exec(`INSERT INTO findings (id, check_code, entity_type, entity_key, severity, state, last_transition, since, since_epoch,
  transitioned_at, last_seen_epoch, updated_epoch, detail) VALUES (?, ?, 'stream', ?, 'info', ?, ?, ?, 1, ?, 1, 1, '')`,
			f[0]+":"+f[1], f[0], f[1], f[2], f[2], t0, t0); err != nil {
			t.Fatal(err)
		}
	}
	// Refused, and nothing stored: an entity without a finding of the
	// check, one of another type than the check's.
	_, err := c.AcceptFindings(ctx, connect.NewRequest(&v1.AcceptFindingsRequest{CheckCode: "stream-005",
		Entities: []*v1.EntityRef{stream(noLimitsKey), stream(account + "/typo")}, Reason: "r", MutedBy: "op", ExpiresAt: timestamppb.New(now.Add(10 * day))}))
	if connect.CodeOf(err) != connect.CodeNotFound || !strings.Contains(err.Error(), account+"/typo") {
		t.Fatalf("entity without a finding: %v", err)
	}
	// A resolved finding: nothing fires to accept.
	_, err = c.AcceptFindings(ctx, connect.NewRequest(&v1.AcceptFindingsRequest{CheckCode: "stream-005",
		Entities: []*v1.EntityRef{stream(noLimitsKey), stream(account + "/gone")}, Reason: "r", MutedBy: "op", ExpiresAt: timestamppb.New(now.Add(10 * day))}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), account+"/gone") {
		t.Fatalf("resolved finding: %v", err)
	}
	_, err = c.MuteFinding(ctx, connect.NewRequest(&v1.MuteFindingRequest{CheckCode: "stream-005", Accept: true,
		Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_CONSUMER, Key: account + "/catalog-changes/x"}, Reason: "r", MutedBy: "op", ExpiresAt: timestamppb.New(now.Add(10 * day))}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("consumer for stream-005: %v", err)
	}
	if n := must(c.ListAcceptances(ctx, connect.NewRequest(&v1.ListAcceptancesRequest{IncludeEnded: true}))).GetTotalCount(); n != 0 {
		t.Fatalf("refused calls stored %d acceptances", n)
	}

	res, err := c.AcceptFindings(ctx, connect.NewRequest(&v1.AcceptFindingsRequest{CheckCode: "stream-005",
		Entities: []*v1.EntityRef{stream(noLimitsKey), stream("OTHER/archive")}, Reason: "kept on purpose", MutedBy: "ops@example.com",
		ExpiresAt: timestamppb.New(now.Add(90 * day))}))
	if err != nil {
		t.Fatal(err)
	}
	ms := res.Msg.GetMutes()
	if len(ms) != 2 || ms[0].GetEntity().GetKey() != noLimitsKey || ms[1].GetEntity().GetKey() != "OTHER/archive" ||
		!ms[0].GetAccepted() || !ms[0].GetGrowthReview() || ms[0].GetMutedBy() != "ops@example.com" {
		t.Fatalf("accepted = %v", ms)
	}
	// A refused batch stores nothing: the second entity is named twice.
	_, err = c.AcceptFindings(ctx, connect.NewRequest(&v1.AcceptFindingsRequest{CheckCode: "stream-005",
		Entities: []*v1.EntityRef{stream("APP/x"), stream("APP/y"), stream("APP/y")}, Reason: "r", MutedBy: "op",
		ExpiresAt: timestamppb.New(now.Add(10 * day))}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("duplicate entity: %v", err)
	}
	// A later, sooner acceptance through MuteFinding heads the list.
	soon := must(c.MuteFinding(ctx, connect.NewRequest(&v1.MuteFindingRequest{CheckCode: "stream-018", Entity: stream(account + "/single"),
		Accept: true, Reason: "R1 by design", MutedBy: "op", ExpiresAt: timestamppb.New(now.Add(7 * day))}))).GetMute()

	// A second acceptance in force of the same check and entity is refused, alone or in a batch.
	_, err = c.MuteFinding(ctx, connect.NewRequest(&v1.MuteFindingRequest{CheckCode: "stream-005", Entity: stream(noLimitsKey), Accept: true,
		Reason: "r", MutedBy: "op", ExpiresAt: timestamppb.New(now.Add(10 * day))}))
	if connect.CodeOf(err) != connect.CodeAlreadyExists {
		t.Fatalf("second acceptance: %v", err)
	}
	_, err = c.AcceptFindings(ctx, connect.NewRequest(&v1.AcceptFindingsRequest{CheckCode: "stream-005", Entities: []*v1.EntityRef{stream("OTHER/archive")},
		Reason: "r", MutedBy: "op", ExpiresAt: timestamppb.New(now.Add(10 * day))}))
	if connect.CodeOf(err) != connect.CodeAlreadyExists || !strings.Contains(err.Error(), "OTHER/archive") {
		t.Fatalf("second acceptance in a batch: %v", err)
	}

	// A plain mute of an accepted entity is refused: the acceptance would
	// hide behind it.
	_, err = c.MuteFinding(ctx, connect.NewRequest(&v1.MuteFindingRequest{CheckCode: "stream-005", Entity: stream(noLimitsKey),
		Reason: "r", MutedBy: "op", ExpiresAt: timestamppb.New(now.Add(time.Hour))}))
	if connect.CodeOf(err) != connect.CodeAlreadyExists || !strings.Contains(err.Error(), "end the acceptance before muting") {
		t.Fatalf("plain mute of an accepted entity: %v", err)
	}
	list := must(c.ListAcceptances(ctx, connect.NewRequest(&v1.ListAcceptancesRequest{})))
	got := list.GetAcceptances()
	if list.GetTotalCount() != 3 || len(got) != 3 || got[0].GetId() != soon.GetId() || got[1].GetId() != ms[0].GetId() {
		t.Fatalf("review list = %v", got)
	}
	scoped := must(c.ListAcceptances(ctx, connect.NewRequest(&v1.ListAcceptancesRequest{Account: account})))
	if scoped.GetTotalCount() != 2 || scoped.GetOtherAccountCount() != 1 {
		t.Fatalf("scoped to %s: total %d, others %d", account, scoped.GetTotalCount(), scoped.GetOtherAccountCount())
	}
	page := must(c.ListAcceptances(ctx, connect.NewRequest(&v1.ListAcceptancesRequest{PageSize: 2})))
	if len(page.GetAcceptances()) != 2 || page.GetNextPageToken() == "" {
		t.Fatalf("first page = %v", page)
	}
	next := must(c.ListAcceptances(ctx, connect.NewRequest(&v1.ListAcceptancesRequest{PageSize: 2, PageToken: page.GetNextPageToken()})))
	if len(next.GetAcceptances()) != 1 || next.GetAcceptances()[0].GetEntity().GetKey() != "OTHER/archive" || next.GetNextPageToken() != "" {
		t.Fatalf("second page = %v", next)
	}

	// Ended: hidden by default, listed after those in force with include_ended.
	must(c.UnmuteFinding(ctx, connect.NewRequest(&v1.UnmuteFindingRequest{MuteId: soon.GetId(), UnmutedBy: "lead"})))
	if n := must(c.ListAcceptances(ctx, connect.NewRequest(&v1.ListAcceptancesRequest{}))).GetTotalCount(); n != 2 {
		t.Fatalf("in force after an end = %d", n)
	}
	all := must(c.ListAcceptances(ctx, connect.NewRequest(&v1.ListAcceptancesRequest{IncludeEnded: true}))).GetAcceptances()
	if len(all) != 3 || all[2].GetId() != soon.GetId() || all[2].GetUnmutedAt() == nil {
		t.Fatalf("with ended = %v", all)
	}

	tally := must(c.GetShellStatus(ctx, connect.NewRequest(&v1.GetShellStatusRequest{}))).GetTally()
	if tally.GetAccepted() != 2 || tally.GetMuted() != 2 {
		t.Errorf("tally = %v, want catalog-changes and OTHER/archive accepted and counted within muted", tally)
	}
}
