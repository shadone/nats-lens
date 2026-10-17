package storeprov_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	"github.com/shadone/nats-lens/internal/checks"
	"github.com/shadone/nats-lens/internal/config"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/gen/natslens/v1/natslensv1connect"
	"github.com/shadone/nats-lens/internal/inventory"
	"github.com/shadone/nats-lens/internal/store"
)

const fixtureInventory = `accounts:
  - name: APP
    streams:
      - name: order-jobs
        consumers:
          - name: resize-worker
          - name: index-worker
          - name: job-cache-projector
            max_deliver: 5
`

const (
	missingKey  = account + "/order-jobs/job-cache-projector"
	missingID   = "consumer-010:" + missingKey
	backlogKey  = account + "/order-jobs/index-worker"
	serverID    = "server-002:nats-0"
	noLimitsKey = account + "/catalog-changes"
)

// findingsStore is the fixture with an inventory and synthetic check runs
// for its three epochs:
//   - consumer-010 for an expected consumer no server reports, from epoch 2,
//     caused by the fixture's consumer.deleted event, warning in epoch 3;
//   - server-002 for nats-0 in epoch 2 only;
//   - stream-005 (info) for catalog-changes in every epoch;
//   - consumer-004 fails to run in epoch 3.
func findingsStore(t *testing.T) (*store.Store, *checks.Set, int64) {
	t.Helper()
	st := fixtureStore(t)
	path := filepath.Join(t.TempDir(), "inventory.yaml")
	if err := os.WriteFile(path, []byte(fixtureInventory), 0o600); err != nil {
		t.Fatal(err)
	}
	rows, err := inventory.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceInventory(ctx, rows, path, t0); err != nil {
		t.Fatal(err)
	}
	set, err := checks.Load(config.Checks{})
	if err != nil {
		t.Fatal(err)
	}
	var deleted int64
	if err := st.DB().QueryRow(`SELECT id FROM events WHERE kind = 'consumer.deleted'`).Scan(&deleted); err != nil {
		t.Fatal(err)
	}
	row := func(key, sev, detail string) store.FindingRow {
		return store.FindingRow{EntityKey: key, Severity: sev, Detail: detail}
	}
	for epoch := int64(1); epoch <= 3; epoch++ {
		var runs []store.CheckRun
		for _, c := range set.Checks() {
			run := store.CheckRun{Code: c.Code, EntityType: c.Entity, Status: store.RunOK}
			switch {
			case c.Code == "consumer-010" && epoch >= 2:
				r := row(missingKey, "critical", "Consumer job-cache-projector on order-jobs is in the inventory and no server reports it. Deleted by load-churn.")
				if epoch == 3 {
					r.Severity = "warning"
				}
				r.CauseEventID = deleted
				r.RelatedConfig, r.RelatedSource = map[string]string{"max_deliver": "5", "durable_name": "job-cache-projector"}, "inventory"
				run.Rows = []store.FindingRow{r}
			case c.Code == "server-002" && epoch == 2:
				v := 2.0
				r := row("nats-0", "critical", "nats-0 healthz unavailable")
				r.Value = &v
				run.Rows = []store.FindingRow{r}
			case c.Code == "stream-005":
				run.Rows = []store.FindingRow{row(noLimitsKey, "info", "catalog-changes has no limits")}
			case c.Code == "consumer-004" && epoch == 3:
				run.Status, run.Error = store.RunError, "timeout"
			}
			runs = append(runs, run)
		}
		if _, err := st.ApplyCheckRuns(ctx, epoch, runs); err != nil {
			t.Fatal(err)
		}
	}
	return st, set, deleted
}

func findingsClient(t *testing.T) (natslensv1connect.LensServiceClient, int64) {
	t.Helper()
	st, set, deleted := findingsStore(t)
	_, c := apitest.Serve(t, &storeprov.Provider{St: st, Interval: 30 * time.Second, Live: true, Checks: set, Clock: func() time.Time { return now }})
	return c, deleted
}

func TestFindingsFromTheChecks(t *testing.T) {
	c, deleted := findingsClient(t)
	list := must(c.ListFindings(ctx, connect.NewRequest(&v1.ListFindingsRequest{Range: hour()})))
	if list.GetTotalCount() != 2 || list.GetFindings()[0].GetId() != missingID {
		t.Fatalf("firing findings = %v", list.GetFindings())
	}
	f := list.GetFindings()[0]
	if f.GetCheckName() != "Expected consumer missing" || f.GetSeverity() != v1.Severity_SEVERITY_WARNING ||
		f.GetState() != v1.FindingState_FINDING_STATE_CHANGED || f.GetRemediation() == "" ||
		f.GetEntity().GetConsumer() != "job-cache-projector" || f.GetRelatedConfigSource() != "inventory" {
		t.Fatalf("consumer-010 = %v", f)
	}
	if f.GetCause().GetId() != itoa(deleted) || f.GetCause().GetKind() != "consumer.deleted" {
		t.Fatalf("cause = %v, want event %d", f.GetCause(), deleted)
	}
	if len(f.GetRelatedConfig()) != 2 || f.GetRelatedConfig()[0].GetKey() != "durable_name" {
		t.Fatalf("related config = %v", f.GetRelatedConfig())
	}
	resolved := must(c.ListFindings(ctx, connect.NewRequest(&v1.ListFindingsRequest{Range: hour(),
		States: []v1.FindingState{v1.FindingState_FINDING_STATE_RESOLVED}})))
	if resolved.GetTotalCount() != 1 || resolved.GetFindings()[0].GetId() != serverID {
		t.Fatalf("resolved = %v", resolved.GetFindings())
	}
	r := resolved.GetFindings()[0]
	if r.GetValue().GetDisplay() != "2" || r.GetValue().GetName() != "healthz errors" || r.GetValue().GetBasis() == "" {
		t.Fatalf("server-002 value = %v", r.GetValue())
	}
	g := must(c.GetFinding(ctx, connect.NewRequest(&v1.GetFindingRequest{Id: missingID})))
	if len(g.GetTransitions()) != 2 || g.GetTransitions()[0].GetState() != v1.FindingState_FINDING_STATE_CHANGED ||
		g.GetTransitions()[1].GetState() != v1.FindingState_FINDING_STATE_FIRING {
		t.Fatalf("transitions = %v", g.GetTransitions())
	}
	if _, err := c.GetFinding(ctx, connect.NewRequest(&v1.GetFindingRequest{Id: "x-001:y"})); codeOf(err) != connect.CodeNotFound {
		t.Fatalf("unknown finding: %v", err)
	}
	byEntity := must(c.ListFindings(ctx, connect.NewRequest(&v1.ListFindingsRequest{Range: hour(),
		Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_STREAM, Key: account + "/order-jobs"}})))
	if byEntity.GetTotalCount() != 1 {
		t.Fatalf("order-jobs and its consumers = %v", byEntity.GetFindings())
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestOverviewAndStatusesFromTheChecks(t *testing.T) {
	c, _ := findingsClient(t)
	o := must(c.GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{Range: hour(), MaxPoints: 120})))
	if o.GetWorstFinding().GetId() != missingID {
		t.Fatalf("worst = %v", o.GetWorstFinding())
	}
	if tl := o.GetTally(); tl.GetWarning() != 1 || tl.GetInfo() != 1 || tl.GetCritical() != 0 || tl.GetResolved_24H() != 1 {
		t.Fatalf("tally = %v", tl)
	}
	for _, tile := range o.GetTiles() {
		// The info finding is on a plain stream; no check covers accounts.
		want := map[string]v1.Status{"consumers": v1.Status_STATUS_WARNING, "streams": v1.Status_STATUS_INFO,
			"accounts": v1.Status_STATUS_NO_DATA}[tile.GetId()]
		if want == v1.Status_STATUS_UNSPECIFIED {
			want = v1.Status_STATUS_OK
		}
		if tile.Value != nil && tile.GetStatus() != want {
			t.Errorf("tile %s status %v, want %v", tile.GetId(), tile.GetStatus(), want)
		}
	}
	for _, s := range o.GetServers() {
		if s.GetStatus() != v1.Status_STATUS_OK {
			t.Errorf("server %s = %v after server-002 resolved", s.GetName(), s.GetStatus())
		}
	}
	streams := must(c.ListStreams(ctx, connect.NewRequest(&v1.ListStreamsRequest{Range: hour()}))).GetStreams()
	got := map[string]*v1.StreamSummary{}
	for _, s := range streams {
		got[s.GetEntity().GetName()] = s
	}
	jobs, rec, other := got["order-jobs"], got["catalog-changes"], got["order-archive"]
	if jobs.GetStatus() != v1.Status_STATUS_WARNING || strings.Join(jobs.GetFindingCodes(), ",") != "consumer-010" ||
		jobs.GetConsumersExpected() != 3 || jobs.GetConsumers() != 2 || streams[0].GetEntity().GetName() != "order-jobs" {
		t.Fatalf("order-jobs = %v", jobs)
	}
	if rec.GetStatus() != v1.Status_STATUS_INFO || rec.ConsumersExpected != nil || other.GetStatus() != v1.Status_STATUS_OK {
		t.Fatalf("catalog-changes %v, order-archive %v", rec, other)
	}
	// Limit 1 gives the worst stream alone, with the count of all.
	one := must(c.ListStreams(ctx, connect.NewRequest(&v1.ListStreamsRequest{Range: hour(), Limit: 1})))
	if len(one.GetStreams()) != 1 || one.GetStreams()[0].GetEntity().GetName() != "order-jobs" || one.GetStreams()[0].GetStatus() != v1.Status_STATUS_WARNING ||
		one.GetTotalCount() != int32(len(streams)) || len(streams) < 3 {
		t.Fatalf("limit 1: %v, total count %d of %d", one.GetStreams(), one.GetTotalCount(), len(streams))
	}
	// A limit keeps the worst first; the count and the totals still cover every stream.
	all := must(c.ListStreams(ctx, connect.NewRequest(&v1.ListStreamsRequest{Range: hour()})))
	top := must(c.ListStreams(ctx, connect.NewRequest(&v1.ListStreamsRequest{Range: hour(), Limit: 2})))
	if len(top.GetStreams()) != 2 || top.GetStreams()[0].GetEntity().GetName() != "order-jobs" || top.GetStreams()[1].GetEntity().GetName() != streams[1].GetEntity().GetName() ||
		top.GetTotalCount() != int32(len(streams)) || all.GetTotalCount() != int32(len(streams)) || top.GetTotals().GetBytes() != all.GetTotals().GetBytes() {
		t.Fatalf("limit 2: %d streams, total count %d of %d, bytes %d of %d", len(top.GetStreams()), top.GetTotalCount(), len(streams), top.GetTotals().GetBytes(), all.GetTotals().GetBytes())
	}
	cons := must(c.ListConsumers(ctx, connect.NewRequest(&v1.ListConsumersRequest{Range: hour(), Stream: "order-jobs"}))).GetConsumers()
	// consumer-004 failed in epoch 3, so a consumer without a finding has
	// no data rather than ok.
	if len(cons) != 3 || cons[0].GetEntity().GetKey() != missingKey || cons[0].GetPresent() || cons[0].GetStatus() != v1.Status_STATUS_WARNING ||
		cons[1].GetStatus() != v1.Status_STATUS_NO_DATA {
		t.Fatalf("consumers = %v", cons)
	}
	gc := must(c.GetConsumer(ctx, connect.NewRequest(&v1.GetConsumerRequest{Account: account, Stream: "order-jobs",
		Consumer: "job-cache-projector", Range: hour()})))
	if gc.GetConsumer().GetPresent() || len(gc.GetServers()) != 0 || len(gc.GetFindings()) != 1 || len(gc.GetConfig()) == 0 {
		t.Fatalf("missing consumer = %v", gc)
	}
	gs := must(c.GetStream(ctx, connect.NewRequest(&v1.GetStreamRequest{Account: account, Stream: "order-jobs", Range: hour()})))
	if len(gs.GetConsumers()) != 3 || len(gs.GetFindings()) != 1 {
		t.Fatalf("stream detail: %d consumers, findings %v", len(gs.GetConsumers()), gs.GetFindings())
	}
}

func TestTimelineAndEventsFromTheChecks(t *testing.T) {
	c, deleted := findingsClient(t)
	// One 30-second bucket per epoch.
	r := &v1.TimeRange{Start: timestamppb.New(t0), End: timestamppb.New(t0.Add(90 * time.Second))}
	tl := must(c.GetStatusTimeline(ctx, connect.NewRequest(&v1.GetStatusTimelineRequest{Range: r, Bucket: durationpb.New(30 * time.Second)})))
	rows := map[string]*v1.TimelineRow{}
	for _, row := range tl.GetRows() {
		rows[row.GetKey()] = row
	}
	cells := func(key string) string {
		var out []string
		for _, s := range rows[key].GetCells() {
			out = append(out, strings.TrimPrefix(s.String(), "STATUS_"))
		}
		return strings.Join(out, " ")
	}
	if got := cells("consumer-010"); got != "OK CRITICAL WARNING" || rows["consumer-010"].GetNow() != v1.Status_STATUS_WARNING {
		t.Errorf("consumer-010 cells %q", got)
	}
	if got := cells("server-002"); got != "OK CRITICAL OK" {
		t.Errorf("server-002 cells %q", got)
	}
	if got := cells("stream-005"); got != "INFO INFO INFO" {
		t.Errorf("stream-005 cells %q", got)
	}
	if _, ok := rows["consumer-004"]; ok {
		t.Errorf("consumer-004 never failed, its row should be left out without include_ok")
	}
	if len(tl.GetRows()) != 3 || tl.GetRows()[0].GetCheckCode() != "consumer-010" {
		t.Errorf("rows = %v", tl.GetRows())
	}
	all := must(c.GetStatusTimeline(ctx, connect.NewRequest(&v1.GetStatusTimelineRequest{Range: r, Bucket: durationpb.New(30 * time.Second), IncludeOk: true})))
	for _, row := range all.GetRows() {
		if row.GetCheckCode() == "consumer-004" {
			rows["consumer-004"] = row
		}
	}
	if got := cells("consumer-004"); got != "OK OK NO_DATA" || rows["consumer-004"].GetNow() != v1.Status_STATUS_NO_DATA {
		t.Errorf("consumer-004 (failed in epoch 3) cells %q", got)
	}
	// A mute created now leaves the earlier cells as they were and shows
	// in the row's state now.
	must(c.MuteFinding(ctx, connect.NewRequest(&v1.MuteFindingRequest{CheckCode: "consumer-010", Reason: "test", MutedBy: "op",
		Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_CONSUMER, Key: missingKey}, ExpiresAt: timestamppb.New(now.Add(time.Hour))})))
	tl = must(c.GetStatusTimeline(ctx, connect.NewRequest(&v1.GetStatusTimelineRequest{Range: r, Bucket: durationpb.New(30 * time.Second),
		Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_STREAM, Key: account + "/order-jobs"}})))
	if len(tl.GetRows()) != 1 || tl.GetRows()[0].GetKey() != missingID || tl.GetRows()[0].GetCells()[2] != v1.Status_STATUS_WARNING ||
		tl.GetRows()[0].GetNow() != v1.Status_STATUS_MUTED {
		t.Errorf("per-entity rows with the mute = %v", tl.GetRows())
	}
	var lane []string
	for _, l := range tl.GetNats() {
		lane = append(lane, l.GetKind())
	}
	if strings.Join(lane, ",") != "consumer.deleted,consumer.created,finding.firing,finding.changed" {
		t.Errorf("lane = %v", lane)
	}

	evs := must(c.ListEvents(ctx, connect.NewRequest(&v1.ListEventsRequest{Range: r,
		Sources: []v1.EventSource{v1.EventSource_EVENT_SOURCE_FINDING}}))).GetEvents()
	// consumer-010 firing and changed, server-002 firing and resolved,
	// stream-005 firing.
	if len(evs) != 5 {
		t.Fatalf("finding events = %d, want 5", len(evs))
	}
	var fired, resolvedEv *v1.Event
	for _, e := range evs {
		if e.GetKind() == "finding.firing" && e.GetTransition().GetFindingId() == missingID {
			fired = e
		}
		if e.GetKind() == "finding.resolved" && e.GetTransition().GetFindingId() == serverID {
			resolvedEv = e
		}
	}
	if want := "resolved: nats-0. Last failing result: nats-0 healthz unavailable"; resolvedEv.GetSummary() != want {
		t.Errorf("resolved event summary %q, want %q", resolvedEv.GetSummary(), want)
	}
	if want := "job-cache-projector on order-jobs: Consumer job-cache-projector"; !strings.HasPrefix(fired.GetSummary(), want) {
		t.Errorf("finding event summary %q, want it to name the entity first", fired.GetSummary())
	}
	if fired == nil || fired.GetType() != "consumer-010 Expected consumer missing" || fired.GetSource() != v1.EventSource_EVENT_SOURCE_FINDING ||
		fired.GetSeverity() != v1.Severity_SEVERITY_CRITICAL || fired.GetFinding().GetId() != missingID || fired.GetConsumer() != "job-cache-projector" {
		t.Fatalf("finding.firing event = %v", fired)
	}
	got := must(c.GetEvent(ctx, connect.NewRequest(&v1.GetEventRequest{Id: fired.GetId()}))).GetEvent()
	if !strings.Contains(got.GetRaw(), missingID) || got.GetTransition().GetState() != v1.FindingState_FINDING_STATE_FIRING {
		t.Fatalf("GetEvent = %v", got)
	}
	cause := must(c.GetEvent(ctx, connect.NewRequest(&v1.GetEventRequest{Id: itoa(deleted)}))).GetEvent()
	if len(cause.GetRaised()) != 1 || cause.GetRaised()[0].GetId() != missingID {
		t.Fatalf("raised by the delete = %v", cause.GetRaised())
	}
}
