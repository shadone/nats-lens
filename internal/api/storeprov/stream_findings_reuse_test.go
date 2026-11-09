package storeprov

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
)

type streamEvidenceDB struct {
	*sql.DB
	causeReads      int
	afterFirstCause func() error
}

func (d *streamEvidenceDB) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	if strings.Contains(q, "FROM events e WHERE e.id IN (") {
		d.causeReads++
		if d.causeReads == 1 && d.afterFirstCause != nil {
			if err := d.afterFirstCause(); err != nil {
				return nil, err
			}
		}
	}
	return d.DB.QueryContext(ctx, q, args...)
}
func streamEvidenceProvider(t *testing.T) (*Provider, *streamEvidenceDB, *v1.GetStreamRequest) {
	t.Helper()
	p, _, r := pageScaleProvider(t, 1)
	ctx := context.Background()
	at := r.End.AsTime().Add(-2 * p.Interval)
	if _, err := p.St.DB().ExecContext(ctx, `INSERT INTO events(id,time,received_at,source,kind,summary) VALUES(700,?,?,'collector','collector.connected','fixture cause')`, at, at); err != nil {
		t.Fatal(err)
	}
	if _, err := p.St.ApplyCheckRuns(ctx, 37, []store.CheckRun{{Code: "stream-005", EntityType: "stream", Status: store.RunOK, Rows: []store.FindingRow{{EntityKey: "APP/orders-00000", Severity: "warning", Detail: "original evidence", CauseEventID: 700}}}}); err != nil {
		t.Fatal(err)
	}
	spy := &streamEvidenceDB{DB: p.St.DB()}
	p.readDB = spy
	return p, spy, &v1.GetStreamRequest{Account: "APP", Stream: "orders-00000", Range: r}
}
func TestGetStreamSingleFiringEvidenceRead(t *testing.T) {
	p, spy, req := streamEvidenceProvider(t)
	response, err := p.GetStream(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if spy.causeReads != 1 {
		t.Fatalf("cause-event lookup made %d queries; one request-local firing read should make one", spy.causeReads)
	}
	if response.GetStream().GetStatus() != v1.Status_STATUS_WARNING || len(response.Findings) != 1 || response.Findings[0].GetCause().GetId() != "700" {
		t.Fatalf("evidence lost: %v", response)
	}
}
func TestGetStreamStatusAndFindingUseSameEvidence(t *testing.T) {
	p, spy, req := streamEvidenceProvider(t)
	spy.afterFirstCause = func() error {
		_, err := p.St.DB().Exec(`UPDATE findings SET severity='critical',detail='later evidence' WHERE check_code='stream-005'`)
		return err
	}
	response, err := p.GetStream(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if response.GetStream().GetStatus() != v1.Status_STATUS_WARNING || len(response.Findings) != 1 || response.Findings[0].GetSeverity() != v1.Severity_SEVERITY_WARNING || response.Findings[0].GetDetail() != "original evidence" {
		t.Fatalf("status and detail straddle different reads: status %v; findings %v", response.GetStream().GetStatus(), response.Findings)
	}
	current, err := p.St.Findings(context.Background(), p.Now(), store.FindingQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(current) != 1 || current[0].Severity != "critical" {
		t.Fatalf("fixture mutation not applied: %v", current)
	}
}
