package storeprov

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
)

// consumerEvidenceDB runs beforeChecksRan once, as GetConsumer reads which
// checks ran: after the consumer's status findings, before its reply is built.
type consumerEvidenceDB struct {
	*sql.DB
	checkRunReads   int
	beforeChecksRan func() error
}

func (d *consumerEvidenceDB) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	if strings.Contains(q, "FROM check_runs WHERE epoch = ?") {
		d.checkRunReads++
		if d.checkRunReads == 1 && d.beforeChecksRan != nil {
			if err := d.beforeChecksRan(); err != nil {
				return nil, err
			}
		}
	}
	return d.DB.QueryContext(ctx, q, args...)
}

func TestGetConsumerStatusAndFindingUseSameEvidence(t *testing.T) {
	p, _, r := pageScaleProvider(t, 1)
	ctx := context.Background()
	at := r.End.AsTime().Add(-2 * p.Interval)
	if _, err := p.St.DB().ExecContext(ctx, `INSERT INTO events(id,time,received_at,source,kind,summary) VALUES(700,?,?,'collector','collector.connected','fixture cause')`, at, at); err != nil {
		t.Fatal(err)
	}
	const key = "APP/orders-00000/worker-00000"
	if _, err := p.St.ApplyCheckRuns(ctx, 37, []store.CheckRun{{Code: "consumer-001", EntityType: "consumer", Status: store.RunOK, Rows: []store.FindingRow{{EntityKey: key, Severity: "warning", Detail: "original evidence", CauseEventID: 700}}}}); err != nil {
		t.Fatal(err)
	}
	spy := &consumerEvidenceDB{DB: p.St.DB()}
	p.readDB = spy
	spy.beforeChecksRan = func() error {
		// Publish a real later check run, including its transition, between reads.
		later := r.End.AsTime().Add(-p.Interval)
		if _, err := p.St.DB().Exec(`INSERT INTO epochs(epoch,started_at,finished_at,duration_ms,expected_servers,observed_servers,expected_names,observed_names,missing_names,endpoint_failures,partial,errors) VALUES(38,?,?,0,1,1,['nats-0'],['nats-0'],[],0,false,[])`, later, later); err != nil {
			return err
		}
		_, err := p.St.ApplyCheckRuns(context.Background(), 38, []store.CheckRun{{Code: "consumer-001", EntityType: "consumer", Status: store.RunOK, Rows: []store.FindingRow{{EntityKey: key, Severity: "critical", Detail: "later evidence", CauseEventID: 700}}}})
		return err
	}
	response, err := p.GetConsumer(ctx, &v1.GetConsumerRequest{Account: "APP", Stream: "orders-00000", Consumer: "worker-00000", Range: r})
	if err != nil {
		t.Fatal(err)
	}
	if spy.checkRunReads == 0 {
		t.Fatal("the fixture's mutation never ran")
	}
	if response.GetConsumer().GetStatus() != v1.Status_STATUS_WARNING || len(response.Findings) != 1 || response.Findings[0].GetSeverity() != v1.Severity_SEVERITY_WARNING || response.Findings[0].GetDetail() != "original evidence" || response.Findings[0].GetCause().GetId() != "700" {
		t.Fatalf("status and detail straddle different reads: status %v; findings %v", response.GetConsumer().GetStatus(), response.Findings)
	}
	current, err := p.St.Findings(ctx, p.Now(), store.FindingQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(current) != 1 || current[0].Severity != "critical" || current[0].LastTransition != store.TransitionChanged {
		t.Fatalf("fixture mutation not applied: %v", current)
	}
}
