package storeprov_test

import (
	"fmt"
 "context"
	"io"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
)

func scrape(t *testing.T, url string) string {
	t.Helper()
	res, err := httpGet(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/plain; version=0.0.4") {
		t.Fatalf("/metrics: %d %s\n%s", res.StatusCode, res.Header.Get("Content-Type"), b)
	}
	return string(b)
}

func TestMetrics(t *testing.T) {
	st, set, _ := findingsStore(t)
	srv, c := apitest.Serve(t, &storeprov.Provider{St: st, Interval: 30 * time.Second, Live: true, Checks: set, Clock: func() time.Time { return now }})
	body := scrape(t, srv.URL+"/metrics")
	id := `system="default",code="consumer-010",entity_type="consumer",entity_key="` + missingKey + `"`
	for _, want := range []string{
		`nats_lens_finding{` + id + `,muted="false"} 1`,
		`nats_lens_finding_severity{` + id + `} 2`,
		`nats_lens_finding_flapping{` + id + `} 0`,
		`nats_lens_finding_severity{system="default",code="stream-005",entity_type="stream",entity_key="` + noLimitsKey + `"} 1`,
		`nats_lens_findings_by_check{system="default",code="consumer-010"} 1`,
		`nats_lens_findings_by_check_max_severity{system="default",code="consumer-010"} 2`,
		`nats_lens_findings_total{system="default",severity="critical"} 0`,
		`nats_lens_findings_total{system="default",severity="warning"} 1`,
		`nats_lens_findings_muted{system="default"} 0`,
		`nats_lens_last_epoch_age_seconds{system="default"} 30`,
		`nats_lens_last_epoch_partial{system="default"} 1`,
		`nats_lens_partial_epochs_last_hour{system="default"} 3`,
		`nats_lens_epochs_without_check_runs_last_hour{system="default"} 0`,
		`nats_lens_check_run_errors_last_hour{system="default"} 1`,
		`nats_lens_check_failing{system="default",code="consumer-004"} 1`,
		`# TYPE nats_lens_event_messages_dropped_last_hour gauge`,
	} {
		if !strings.Contains(body, want+"\n") {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(body, `code="server-002"`) {
		t.Error("a resolved finding is exported")
	}
	// A mute keeps the finding's series, marked muted, with the same
	// identity; it leaves the per-check counts.
	must(c.MuteFinding(ctx, connect.NewRequest(&v1.MuteFindingRequest{CheckCode: "consumer-010", Reason: "r", MutedBy: "op",
		ExpiresAt: timestamppb.New(now.Add(time.Hour))})))
	body = scrape(t, srv.URL+"/metrics")
	for _, want := range []string{
		`nats_lens_finding{` + id + `,muted="true"} 1`,
		`nats_lens_finding_severity{` + id + `} 2`,
		`nats_lens_findings_muted{system="default"} 1`,
		`nats_lens_findings_total{system="default",severity="warning"} 0`,
	} {
		if !strings.Contains(body, want+"\n") {
			t.Errorf("after the mute, missing %q", want)
		}
	}
	if strings.Contains(body, `nats_lens_findings_by_check{system="default",code="consumer-010"}`) {
		t.Error("a muted finding is counted per check")
	}
}

// Label values are escaped: quotes, backslashes and newlines in an entity
// key stay inside their label.
func TestMetricsEscapeLabels(t *testing.T) {
	st, set, _ := findingsStore(t)
	applyEpoch(t, st, set, 4, t0.Add(90*time.Second), func(code string) (string, []store.FindingRow) {
		if code == "client-001" {
			return "", []store.FindingRow{{EntityKey: "a\"b\\c\nd", Severity: "warning", Detail: "x"}}
		}
		return "", nil
	})
	p := &storeprov.Provider{St: st, Interval: 30 * time.Second, Checks: set, Clock: func() time.Time { return t0.Add(2 * time.Minute) }}
	body := writeMetrics(t, p)
	if want := `nats_lens_finding{system="default",code="client-001",entity_type="client",entity_key="a\"b\\c\nd",muted="false"} 1` + "\n"; !strings.Contains(body, want) {
		t.Fatalf("missing %q in\n%s", want, body)
	}
}

// One check's entity series are capped; the per-check count stays whole.
func TestMetricsCapEntitySeries(t *testing.T) {
	st, set, _ := findingsStore(t)
	addEpochAt(t, st, 4, t0.Add(90*time.Second))
	var runs []store.CheckRun
	for _, c := range set.Checks() {
		run := store.CheckRun{Code: c.Code, EntityType: c.Entity, Status: store.RunOK}
		if c.Code == "client-001" {
			for i := range storeprov.MaxFindingSeries + 7 {
				run.Rows = append(run.Rows, store.FindingRow{EntityKey: fmt.Sprintf("client-%03d", i), Severity: "warning", Detail: "slow"})
			}
		}
		runs = append(runs, run)
	}
	if _, err := st.ApplyCheckRuns(ctx, 4, runs); err != nil {
		t.Fatal(err)
	}
	srv, _ := apitest.Serve(t, &storeprov.Provider{St: st, Interval: 30 * time.Second, Checks: set, Clock: func() time.Time { return now }})
	body := scrape(t, srv.URL+"/metrics")
	if n := strings.Count(body, `nats_lens_finding_severity{system="default",code="client-001"`); n != storeprov.MaxFindingSeries {
		t.Errorf("client-001 series = %d, want %d", n, storeprov.MaxFindingSeries)
	}
	for _, want := range []string{
		`nats_lens_findings_by_check{system="default",code="client-001"} 57`,
		`nats_lens_findings_by_check_max_severity{system="default",code="client-001"} 2`,
		`nats_lens_finding_series_omitted{system="default",code="client-001"} 7`,
	} {
		if !strings.Contains(body, want+"\n") {
			t.Errorf("missing %q", want)
		}
	}
}

// Epochs whose checks never ran are counted, the newest interval aside:
// the fixture's three epochs have no check runs, and the newest is 30
// seconds old.
func TestMetricsCountEpochsWithoutCheckRuns(t *testing.T) {
	p := &storeprov.Provider{St: fixtureStore(t), Interval: 30 * time.Second, Clock: func() time.Time { return now }}
	body := writeMetrics(t, p)
	if !strings.Contains(body, `nats_lens_epochs_without_check_runs_last_hour{system="default"} 2`+"\n") {
		t.Fatalf("metrics:\n%s", body)
	}
}

// The cap picks entities by severity, then entity key, whatever fired
// first, so the set is stable; muted findings are always exported and do
// not count against it.
func TestMetricsCapIsStableAndLeavesMuted(t *testing.T) {
	st, set, _ := findingsStore(t)
	rows := func(prefix string, n int, sev string) []store.FindingRow {
		var out []store.FindingRow
		for i := range n {
			out = append(out, store.FindingRow{EntityKey: fmt.Sprintf("%s%02d", prefix, i), Severity: sev, Detail: "slow"})
		}
		return out
	}
	applyEpoch(t, st, set, 4, t0.Add(90*time.Second), func(code string) (string, []store.FindingRow) {
		if code == "client-001" {
			return "", append(rows("z", 30, "warning"), rows("m", 5, "warning")...)
		}
		return "", nil
	})
	applyEpoch(t, st, set, 5, t0.Add(120*time.Second), func(code string) (string, []store.FindingRow) {
		if code == "client-001" {
			return "", append(append(rows("z", 30, "warning"), rows("m", 5, "warning")...), rows("a", 30, "warning")...)
		}
		return "", nil
	})
	if _, err := st.CreateMute(ctx, store.Mute{CheckCode: "client-001", EntityType: "client", EntityKey: "m00", Reason: "r", MutedBy: "op",
		CreatedAt: t0, ExpiresAt: t0.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	p := &storeprov.Provider{St: st, Interval: 30 * time.Second, Checks: set, Clock: func() time.Time { return t0.Add(130 * time.Second) }}
	body := writeMetrics(t, p)
	has := func(key string) bool {
		return strings.Contains(body, `nats_lens_finding_severity{system="default",code="client-001",entity_type="client",entity_key="`+key+`"}`)
	}
	if !has("a29") || !has("m04") || has("z29") || !has("m00") {
		t.Errorf("a29 %v, m04 %v, z29 %v, muted m00 %v", has("a29"), has("m04"), has("z29"), has("m00"))
	}
	if !strings.Contains(body, `nats_lens_finding_series_omitted{system="default",code="client-001"} 14`+"\n") {
		t.Errorf("omitted: %s", body)
	}
}

// writeMetrics is the provider's exposition as system default.
func writeMetrics(t *testing.T, p *storeprov.Provider) string {
	t.Helper()
	m, err := api.NewExposition(api.DefaultSystemID)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.WriteMetrics(ctx, m); err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	if _, err := m.WriteTo(&sb); err != nil {
		t.Fatal(err)
	}
	return sb.String()
}

func TestNotificationMetrics(t *testing.T) {
 st,set,_:=findingsStore(t)
 ctx:=context.Background()
 for i,state:=range []string{"pending","delivered","refused","failed"} {
  if _,err:=st.DB().ExecContext(ctx,`INSERT INTO notify_outbox(id,channel,created_at,epoch,kind,dedup_key,message,state,finished_at) VALUES (?, 'ops', ?, 1, 'baseline', ?, '{}', ?, ?)`,i+1,now.Add(-2*time.Hour),fmt.Sprint(i),state,now.Add(-time.Minute));err!=nil{t.Fatal(err)}
 }
 srv,_:=apitest.Serve(t,&storeprov.Provider{St:st,Checks:set,Clock:func()time.Time{return now}})
 body:=scrape(t,srv.URL+"/metrics")
 for _,want:=range []string{`nats_lens_notifications_pending{system="default",channel="ops"} 1`,`nats_lens_notifications_delivered_last_hour{system="default",channel="ops"} 1`,`nats_lens_notifications_failed_last_hour{system="default",channel="ops"} 2`,`nats_lens_notifications_oldest_pending_seconds{system="default",channel="ops"} 7200`} {if !strings.Contains(body,want+"\n"){t.Errorf("missing %s",want)}}
}
