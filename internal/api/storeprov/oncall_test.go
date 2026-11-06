package storeprov_test

import (
	"connectrpc.com/connect"
	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	"github.com/shadone/nats-lens/internal/checks"
	"github.com/shadone/nats-lens/internal/config"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOnCallStore(t *testing.T) {
	t.Setenv("TZ", "America/Los_Angeles")
	st, set, _ := findingsStore(t)
	p := &storeprov.Provider{St: st, Checks: set, Interval: 30 * time.Second, Clock: func() time.Time { return now }, Kubernetes: true}
	_, c := apitest.Serve(t, p)
	got := must(c.GetOnCall(ctx, connect.NewRequest(&v1.GetOnCallRequest{FindingId: missingID})))
	detail := must(c.GetFinding(ctx, connect.NewRequest(&v1.GetFindingRequest{Id: missingID})))
	if !proto.Equal(got.Finding, detail.Finding) || got.SameCheckFiring != 1 || len(got.Assets) != 1 || got.Assets[0].Online != nil {
		t.Fatalf("finding/absent placement: %v", got)
	}
	if got.Now.GetServersExpected() != 3 || got.Now.GetServersAnswering() != 2 || !got.Now.EpochAt.AsTime().Equal(t0.Add(time.Minute)) {
		t.Fatalf("non-UTC present: %v", got.Now)
	}
	if got.StepsNote == "" || len(got.Steps) != 0 {
		t.Fatalf("short history: %v", got)
	}
	resolved := must(c.GetOnCall(ctx, connect.NewRequest(&v1.GetOnCallRequest{FindingId: serverID})))
	if resolved.Finding.GetAcknowledgement() != nil || !proto.Equal(resolved.StoryB, resolved.Finding.ResolvedAt) || !proto.Equal(resolved.Now.EpochAt, got.Now.EpochAt) {
		t.Fatal(resolved)
	}
	a := must(c.AcknowledgeFinding(ctx, connect.NewRequest(&v1.AcknowledgeFindingRequest{FindingId: missingID, UntilResolved: true, AckedBy: "alice@example.com"})))
	got = must(c.GetOnCall(ctx, connect.NewRequest(&v1.GetOnCallRequest{FindingId: missingID})))
	if !proto.Equal(got.Finding.Acknowledgement, a.Acknowledgement) {
		t.Fatal("current acknowledgement lost")
	}
	// Mutes change the group count without hiding the requested finding.
	must(c.MuteFinding(ctx, connect.NewRequest(&v1.MuteFindingRequest{CheckCode: "consumer-010", Reason: "investigating", MutedBy: "alice@example.com", ExpiresAt: timestamppb.New(now.Add(time.Hour))})))
	got = must(c.GetOnCall(ctx, connect.NewRequest(&v1.GetOnCallRequest{FindingId: missingID})))
	if got.Finding.Mute == nil || got.Finding.Acknowledgement == nil || got.SameCheckFiring != 0 || len(got.Assets) != 1 {
		t.Fatal(got)
	}
}

func TestOnCallPlaybookAndMissingNow(t *testing.T) {
	st, _, _ := findingsStore(t)
	path := filepath.Join(t.TempDir(), "remediation.yaml")
	if err := os.WriteFile(path, []byte("consumer-010: {text: Read the playbook., playbook: 'https://runbooks.example.com/nats/durable-missing'}"), 0600); err != nil {
		t.Fatal(err)
	}
	set, err := checks.Load(config.Checks{RemediationFile: path})
	if err != nil {
		t.Fatal(err)
	}
	_, c := apitest.Serve(t, &storeprov.Provider{St: st, Checks: set, Interval: 30 * time.Second, Retention: time.Hour, Clock: func() time.Time { return now.Add(24 * time.Hour) }})
	got := must(c.GetOnCall(ctx, connect.NewRequest(&v1.GetOnCallRequest{FindingId: missingID})))
	if got.PlaybookUrl != "https://runbooks.example.com/nats/durable-missing" || got.Now.EpochAt != nil || got.Now.ServersAnswering != nil || got.Now.DeliveredPerSecond != nil || len(got.Steps) > 0 || got.StepsNote == "" {
		t.Fatal(got)
	}
}

func TestOnCallEveryEntityWithoutFacts(t *testing.T) {
	st := emptyStore(t)
	r := rows{t, st}
	r.epoch(1, t0)
	var runs []store.CheckRun
	for _, typ := range []string{"system", "server", "stream", "consumer", "account", "client", "connection"} {
		code := "test-" + typ
		runs = append(runs, store.CheckRun{Code: code, EntityType: typ, Status: store.RunOK, Rows: []store.FindingRow{{EntityKey: "missing", Severity: "warning", Detail: "not observed"}}})
	}
	if _, err := st.ApplyCheckRuns(ctx, 1, runs); err != nil {
		t.Fatal(err)
	}
	// Drop the only collection: findings remain addressable, with no current facts.
	if _, err := st.DB().Exec(`DELETE FROM epochs`); err != nil {
		t.Fatal(err)
	}
	_, c := apitest.Serve(t, &storeprov.Provider{St: st, Interval: 30 * time.Second, Clock: func() time.Time { return now }})
	for _, typ := range []string{"system", "server", "stream", "consumer", "account", "client", "connection"} {
		t.Run(typ, func(t *testing.T) {
			got := must(c.GetOnCall(ctx, connect.NewRequest(&v1.GetOnCallRequest{FindingId: "test-" + typ + ":missing"})))
			if got.Now.EpochAt != nil || got.Now.ServersExpected != nil || got.Now.ServersAnswering != nil || got.Now.DeliveredPerSecond != nil || got.Now.StreamMsgsPerSecond != nil || len(got.Assets) != 1 || got.Assets[0].Online != nil || got.Assets[0].Replicas != nil {
				t.Fatal(got)
			}
		})
	}
}

func TestOnCallUsesEpochCounts(t *testing.T) {
	st, set := conformanceStore(t)
	p := &storeprov.Provider{St: st, Checks: set, Interval: confStep, Clock: func() time.Time { return confAt(confEpochs).Add(30 * time.Second) }}
	got, err := p.OnCall(ctx, &v1.GetOnCallRequest{FindingId: "server-001:nats-4"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Now.ServersExpected == nil || *got.Now.ServersExpected != 5 || got.Now.ServersAnswering == nil || *got.Now.ServersAnswering != 4 {
		t.Fatalf("must read the epoch's counts, independent of optional names/VARZ: %v", got.Now)
	}
}

func TestOnCallAcknowledgementEpisode(t *testing.T) {
	t.Setenv("TZ", "Asia/Tokyo")
	st, set, _ := findingsStore(t)
	clock := now
	p := &storeprov.Provider{St: st, Checks: set, Interval: 30 * time.Second, Clock: func() time.Time { return clock }}
	_, c := apitest.Serve(t, p)
	ack := must(c.AcknowledgeFinding(ctx, connect.NewRequest(&v1.AcknowledgeFindingRequest{FindingId: missingID, UntilResolved: true, AckedBy: "alice@example.com", Note: "watching"})))
	first := must(c.GetOnCall(ctx, connect.NewRequest(&v1.GetOnCallRequest{FindingId: missingID})))
	if !proto.Equal(first.Finding.Acknowledgement, ack.Acknowledgement) {
		t.Fatal(first)
	}
	applyEpoch(t, st, set, 4, now.Add(30*time.Second), func(string) (string, []store.FindingRow) { return "", nil })
	clock = now.Add(60 * time.Second)
	applyEpoch(t, st, set, 5, clock, func(code string) (string, []store.FindingRow) {
		if code == "consumer-010" {
			return "", []store.FindingRow{{EntityKey: missingKey, Severity: "critical", Detail: "new episode"}}
		}
		return "", nil
	})
	current := must(c.GetOnCall(ctx, connect.NewRequest(&v1.GetOnCallRequest{FindingId: missingID})))
	if current.Finding.Acknowledgement != nil || current.Finding.Since.AsTime().Equal(first.Finding.Since.AsTime()) {
		t.Fatalf("old acknowledgement crossed episode: %v", current)
	}
	for _, tr := range current.Transitions {
		if tr.Time.AsTime().Before(clock) {
			t.Fatalf("previous episode transition: %v", tr)
		}
	}
	past := must(c.ListFindings(ctx, connect.NewRequest(&v1.ListFindingsRequest{Range: &v1.TimeRange{Start: timestamppb.New(t0), End: timestamppb.New(now.Add(time.Second))}, CheckCode: "consumer-010"})))
	if len(past.Findings) != 1 || past.Findings[0].Acknowledgement == nil || past.Findings[0].Acknowledgement.Id != ack.Acknowledgement.Id {
		t.Fatalf("historical acknowledgement changed: %v", past)
	}
}

func TestOnCallServerAnsweringWithoutVarz(t *testing.T) {
	st := emptyStore(t)
	r := rows{t, st}
	r.server("S0", "nats-0")
	r.epoch(1, t0, "S0")
	if _, err := st.ApplyCheckRuns(ctx, 1, []store.CheckRun{{Code: "server-002", EntityType: "server", Status: store.RunOK, Rows: []store.FindingRow{{EntityKey: "nats-0", Severity: "warning", Detail: "VARZ did not answer"}}}}); err != nil {
		t.Fatal(err)
	}
	p := &storeprov.Provider{St: st, Interval: 30 * time.Second, Clock: func() time.Time { return t0.Add(time.Second) }}
	got, err := p.OnCall(ctx, &v1.GetOnCallRequest{FindingId: "server-002:nats-0"})
	if err != nil {
		t.Fatal(err)
	}
	card := api.FinishOnCall(got, nil, "")
	if len(card.Assets) != 1 || card.Assets[0].Answering == nil || !*card.Assets[0].Answering {
		t.Fatalf("JSZ answered without VARZ: %v", card.Assets)
	}
}
