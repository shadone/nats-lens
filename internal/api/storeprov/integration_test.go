package storeprov_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	"github.com/shadone/nats-lens/internal/checks"
	"github.com/shadone/nats-lens/internal/collect"
	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/events"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/inventory"
	"github.com/shadone/nats-lens/internal/runner"
	"github.com/shadone/nats-lens/internal/store"
	"github.com/shadone/nats-lens/internal/testcluster"
)

const clusterInventory = `accounts:
  - name: APP
    streams:
      - name: order-archive
        replicas: 3
        consumers:
          - name: analytics-aggregator
            max_ack_pending: 8000
`

// The reference page's story on real data: a durable from the inventory
// is deleted by a named client, and the API's overview, tally, timeline
// and event list say so, with the delete as the cause.
func TestReferenceStoryOnTheCluster(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a 3-node cluster")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	c := testcluster.Start(t, 3)
	st, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	invPath := filepath.Join(t.TempDir(), "inventory.yaml")
	if err := os.WriteFile(invPath, []byte(clusterInventory), 0o600); err != nil {
		t.Fatal(err)
	}
	rows, err := inventory.Load(invPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceInventory(ctx, rows, invPath, time.Now()); err != nil {
		t.Fatal(err)
	}

	capture := events.New(events.Options{Writer: st, FlushInterval: 100 * time.Millisecond, Log: log})
	if err := capture.SubscribeSys(c.Connect(0, testcluster.SysUser, testcluster.SysPass)); err != nil {
		t.Fatal(err)
	}
	app := c.Connect(0, testcluster.AppUser, testcluster.AppPass)
	if err := capture.SubscribeAccount("APP", app); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = capture.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	js, err := jetstream.New(app)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := js.CreateStream(ctx, jetstream.StreamConfig{Name: "order-archive", Subjects: []string{"orders.archive.>"}, Replicas: 3})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{Durable: "analytics-aggregator",
		AckPolicy: jetstream.AckExplicitPolicy, MaxAckPending: 8000}); err != nil {
		t.Fatal(err)
	}

	set, err := checks.Load(config.Checks{Timeout: config.Duration(10 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	r := &runner.Runner{
		Collector: collect.New(c.Connect(0, testcluster.SysUser, testcluster.SysPass), collect.Options{
			Window: time.Second, ConnzLimit: 100, Tracker: collect.NewTracker(time.Hour), Log: log,
		}),
		Store: st, Interval: 10 * time.Second, Retention: time.Hour, Log: log,
		Checks: &checks.Engine{Set: set, Log: log},
	}
	if _, err := r.Once(ctx); err != nil {
		t.Fatal(err)
	}

	deleter, err := nats.Connect(c.URL(1), nats.UserInfo(testcluster.AppUser, testcluster.AppPass), nats.Name("analytics-worker"))
	if err != nil {
		t.Fatal(err)
	}
	defer deleter.Close()
	djs, err := jetstream.New(deleter)
	if err != nil {
		t.Fatal(err)
	}
	if err := djs.DeleteConsumer(ctx, "order-archive", "analytics-aggregator"); err != nil {
		t.Fatal(err)
	}
	var deleteID int64
	for deadline := time.Now().Add(20 * time.Second); deleteID == 0; time.Sleep(100 * time.Millisecond) {
		_ = st.DB().QueryRow(`SELECT id FROM events WHERE kind = 'consumer.deleted' AND client_name = 'analytics-worker'`).Scan(&deleteID)
		if deleteID == 0 && time.Now().After(deadline) {
			t.Fatal("no consumer.deleted event naming analytics-worker")
		}
	}
	if _, err := r.Once(ctx); err != nil {
		t.Fatal(err)
	}

	_, cl := apitest.Serve(t, &storeprov.Provider{St: st, Interval: 10 * time.Second, Live: true, Checks: set})
	const id = "consumer-010:APP/order-archive/analytics-aggregator"
	o := must(cl.GetOverview(ctx, connect.NewRequest(&v1.GetOverviewRequest{})))
	w := o.GetWorstFinding()
	if w.GetId() != id || w.GetSeverity() != v1.Severity_SEVERITY_CRITICAL || !strings.Contains(w.GetDetail(), "by analytics-worker") ||
		w.GetCause().GetId() != strconv.FormatInt(deleteID, 10) || w.GetCause().GetKind() != "consumer.deleted" ||
		w.GetEntity().GetStream() != "order-archive" {
		t.Fatalf("worst finding = %v", w)
	}
	// Under load (other packages' clusters running in parallel) a slow
	// answer can raise another critical finding, so the tally is checked
	// against the critical findings listed, which include consumer-010.
	crit := must(cl.ListFindings(ctx, connect.NewRequest(&v1.ListFindingsRequest{Severities: []v1.Severity{v1.Severity_SEVERITY_CRITICAL}})))
	if o.GetTally().GetCritical() != crit.GetTotalCount() || crit.GetFindings()[0].GetId() != id {
		for _, f := range crit.GetFindings() {
			t.Logf("%s %s %s", f.GetId(), f.GetSeverity(), f.GetDetail())
		}
		t.Fatalf("tally = %v", o.GetTally())
	}
	tl := must(cl.GetStatusTimeline(ctx, connect.NewRequest(&v1.GetStatusTimelineRequest{
		Range: &v1.TimeRange{Start: timestamppb.New(time.Now().Add(-15 * time.Minute))}})))
	if len(tl.GetRows()) == 0 || tl.GetRows()[0].GetCheckCode() != "consumer-010" || tl.GetRows()[0].GetNow() != v1.Status_STATUS_CRITICAL {
		t.Fatalf("timeline rows = %v", tl.GetRows())
	}
	evs := must(cl.ListEvents(ctx, connect.NewRequest(&v1.ListEventsRequest{Kinds: []string{"finding.firing", "consumer.deleted"},
		Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_STREAM, Key: "APP/order-archive"}}))).GetEvents()
	var firing, deleted *v1.Event
	for _, e := range evs {
		switch e.GetKind() {
		case "finding.firing":
			if e.GetTransition().GetFindingId() == id {
				firing = e
			}
		case "consumer.deleted":
			deleted = e
		}
	}
	if firing == nil || firing.GetType() != "consumer-010 Expected consumer missing" || firing.GetFinding().GetId() != id {
		t.Fatalf("finding.firing event = %v (events %v)", firing, evs)
	}
	if deleted == nil || deleted.GetClientName() != "analytics-worker" || len(deleted.GetRaised()) != 1 || deleted.GetRaised()[0].GetId() != id {
		t.Fatalf("consumer.deleted event = %v", deleted)
	}
	cons := must(cl.ListConsumers(ctx, connect.NewRequest(&v1.ListConsumersRequest{Stream: "order-archive"}))).GetConsumers()
	if len(cons) != 1 || cons[0].GetPresent() || cons[0].GetStatus() != v1.Status_STATUS_CRITICAL {
		t.Fatalf("consumers = %v", cons)
	}
}
