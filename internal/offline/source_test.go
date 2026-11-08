package offline

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/investigation"
	"github.com/shadone/nats-lens/internal/store"
)

func closedFixture(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "closed.duckdb")
	st, err := store.OpenPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO epochs (epoch,started_at,finished_at,duration_ms,expected_servers,observed_servers,expected_names,observed_names,missing_names,endpoint_failures,partial,errors) VALUES (9007199254740993,TIMESTAMP '2026-10-17 14:29:00',TIMESTAMP '2026-10-17 14:29:01',1000,1,1,['nats-0'],['nats-0'],[],0,false,[])`,
		`INSERT INTO servers (server_id,server_name,cluster,first_epoch,last_epoch,first_seen_at,last_seen_at) VALUES ('S0','nats-0','shop',9007199254740993,9007199254740993,TIMESTAMP '2026-10-17 14:29:00',TIMESTAMP '2026-10-17 14:29:00')`,
		`INSERT INTO server_stats (epoch,server_id,cpu_percent) VALUES (9007199254740993,'S0',12.5)`,
		`INSERT INTO accounts (account,account_name,is_system,first_epoch,last_epoch) VALUES ('APP','Shop',false,9007199254740993,9007199254740993)`,
		`INSERT INTO streams (account,stream,first_epoch,last_epoch) VALUES ('APP','orders',9007199254740993,9007199254740993)`,
		`INSERT INTO stream_config (server_id,account,stream,first_epoch,last_epoch,config_hash,storage,replicas) VALUES ('S0','APP','orders',9007199254740993,9007199254740993,'generic','file',1)`,
		`INSERT INTO stream_stats (epoch,server_id,account,stream,messages,bytes,is_leader,leader) VALUES (9007199254740993,'S0','APP','orders',11,9007199254740993,true,'nats-0')`,
		`INSERT INTO events (id,time,received_at,source,kind,server_name,summary) VALUES (9007199254740993,TIMESTAMP '2026-10-17 14:28:00',TIMESTAMP '2026-10-17 14:28:01','sys','server.shutdown','nats-0','Generic captured shutdown'),(9007199254740994,TIMESTAMP '2026-10-17 14:28:30',TIMESTAMP '2026-10-17 14:28:31','sys','server.shutdown','nats-0','Generic later shutdown'),(9007199254740995,TIMESTAMP '2026-10-17 14:30:00',TIMESTAMP '2026-10-17 14:30:01','sys','server.shutdown','nats-0','After collection reference')`,
		`INSERT INTO findings (id,check_code,entity_type,entity_key,severity,state,last_transition,since,since_epoch,transitioned_at,resolved_at,last_seen_epoch,updated_epoch,detail) VALUES ('server-001:nats-0','server-001','server','nats-0','warning','resolved','resolved',TIMESTAMP '2026-10-17 14:27:00',9007199254740992,TIMESTAMP '2026-10-17 14:28:00',TIMESTAMP '2026-10-17 14:28:00',9007199254740992,9007199254740993,'Captured episode')`,
		`INSERT INTO finding_transitions (finding_id,check_code,entity_type,entity_key,epoch,time,kind,severity,detail) VALUES ('server-001:nats-0','server-001','server','nats-0',9007199254740992,TIMESTAMP '2026-10-17 14:27:00','firing','warning','Captured episode'),('server-001:nats-0','server-001','server','nats-0',9007199254740993,TIMESTAMP '2026-10-17 14:28:00','resolved','warning','Captured episode')`,
	} {
		if _, err = st.DB().ExecContext(ctx, q); err != nil {
			st.Close()
			t.Fatal(err)
		}
	}
	if err = st.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}
func TestOfflineClosedSource(t *testing.T) {
	path := closedFixture(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	session, err := Open(context.Background(), investigation.OfflineSelection{DB: path, System: "shop", Interval: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	got, err := session.Source.Query(context.Background(), "GetLensInfo", []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "2026-10-17T14:29:01Z") {
		t.Fatalf("reference clock: %s", got)
	}
	got, err = session.Source.Query(context.Background(), "GetOverview", []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"9007199254740993"`) {
		t.Fatalf("collection precision: %s", got)
	}
	if strings.Contains(string(got), `"collecting":true`) {
		t.Fatalf("invented runtime: %s", got)
	}
	if _, err = session.Source.Query(context.Background(), "ListServers", []byte(`{"system":"other"}`)); err == nil {
		t.Fatal("wrong system accepted")
	}
	session.Close()
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("source bytes changed")
	}
	if _, err = os.Stat(path + ".wal"); !os.IsNotExist(err) {
		t.Fatalf("WAL created: %v", err)
	}
}

func TestOfflineNoEvaluationDoesNotClaimHealthy(t *testing.T) {
	session, err := Open(context.Background(), investigation.OfflineSelection{DB: closedFixture(t), System: "shop", Interval: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	got, err := session.Source.Query(context.Background(), "ListSystems", []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), `"status":"STATUS_OK"`) {
		t.Fatalf("no checks ran, but summary claims healthy: %s", got)
	}
}

func TestSummaryRequiresEvaluation(t *testing.T) {
	for _, c := range []struct{ status, want string }{{"", "STATUS_NO_DATA"}, {"ok", "STATUS_OK"}, {"error", "STATUS_NO_DATA"}, {"disabled", "STATUS_NO_DATA"}} {
		t.Run("run_"+c.status, func(t *testing.T) {
			path := closedFixture(t)
			if c.status != "" {
				st, err := store.OpenPath(context.Background(), path)
				if err != nil {
					t.Fatal(err)
				}
				_, err = st.DB().Exec(`INSERT INTO check_runs(epoch,check_code,status,rows,duration_ms) VALUES (9007199254740993,'server-001',?,0,1)`, c.status)
				closeErr := st.Close()
				if err != nil || closeErr != nil {
					t.Fatalf("%v %v", err, closeErr)
				}
			}
			session, err := Open(context.Background(), investigation.OfflineSelection{DB: path, System: "shop", Interval: 30 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			got, err := session.Source.Query(context.Background(), "ListSystems", []byte(`{"stripBuckets":1}`))
			if err != nil {
				t.Fatal(err)
			}
			var result struct {
				Systems []struct {
					Status string `json:"status"`
				} `json:"systems"`
			}
			if err = json.Unmarshal(got, &result); err != nil {
				t.Fatal(err)
			}
			if len(result.Systems) != 1 || result.Systems[0].Status != c.want {
				t.Fatalf("%s: %s", c.status, got)
			}
			got, err = session.Source.Query(context.Background(), "ListSystems", []byte(`{"range":{"start":"2026-10-16T00:00:00Z","end":"2026-10-16T01:00:00Z"},"stripBuckets":1}`))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(got), `"status":"STATUS_NO_DATA"`) {
				t.Fatalf("empty range health: %s", got)
			}
		})
	}
}

func TestOfflineEntityContinuationSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	path := closedFixture(t)
	st, err := store.OpenPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO streams BY NAME SELECT * REPLACE ('payments' AS stream) FROM streams WHERE stream='orders'`,
		`INSERT INTO stream_config BY NAME SELECT * REPLACE ('payments' AS stream) FROM stream_config WHERE stream='orders'`,
		`INSERT INTO stream_stats BY NAME SELECT * REPLACE ('payments' AS stream) FROM stream_stats WHERE stream='orders'`,
	} {
		if _, err = st.DB().ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	st.Close()
	selection := investigation.OfflineSelection{DB: path, System: "shop", Interval: 30 * time.Second}
	session, err := Open(ctx, selection)
	if err != nil {
		t.Fatal(err)
	}
	first, err := session.Source.Query(ctx, "ListStreams", []byte(`{"account":"APP","limit":1}`))
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		NextPageToken string `json:"nextPageToken"`
	}
	if err = json.Unmarshal(first, &page); err != nil {
		t.Fatal(err)
	}
	session.Close()
	if page.NextPageToken == "" {
		t.Fatal("fixture must page")
	}
	session, err = Open(ctx, selection)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	request, _ := json.Marshal(map[string]any{"account": "APP", "limit": 1, "pageToken": page.NextPageToken})
	second, err := session.Source.Query(ctx, "ListStreams", request)
	if err != nil {
		t.Fatalf("unchanged closed source cannot continue: %v", err)
	}
	if !strings.Contains(string(second), `"name":"payments"`) {
		t.Fatalf("wrong page: %s", second)
	}
}

func TestOfflineZeroDurationCollectionBounds(t *testing.T) {
	ctx := context.Background()
	path := closedFixture(t)
	st, err := store.OpenPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.DB().Exec(`UPDATE epochs SET finished_at=started_at,duration_ms=0`); err != nil {
		t.Fatal(err)
	}
	st.Close()
	selection := investigation.OfflineSelection{DB: path, System: "shop", Interval: 30 * time.Second}
	session, err := Open(ctx, selection)
	if err != nil {
		t.Fatalf("valid zero-duration collection refused: %v", err)
	}
	defer session.Close()
	defaultResult, err := session.Source.Query(ctx, "GetOverview", []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(defaultResult), `"9007199254740993"`) {
		t.Fatalf("latest sample excluded: %s", defaultResult)
	}
	fixedResult, err := session.Source.Query(ctx, "GetOverview", []byte(`{"range":{"start":"2026-10-17T14:28:00Z","end":"2026-10-17T14:29:00Z"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(fixedResult), `"epoch":"9007199254740993"`) {
		t.Fatalf("explicit exclusive end extended: %s", fixedResult)
	}
}

func TestOfflineReferenceResolutionPrecision(t *testing.T) {
	for _, duration := range []time.Duration{0, time.Microsecond, time.Second} {
		t.Run(duration.String(), func(t *testing.T) {
			ctx := context.Background()
			path := closedFixture(t)
			st, err := store.OpenPath(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			at := time.Date(2026, 10, 17, 14, 29, 0, 0, time.UTC)
			finish := at.Add(duration)
			if _, err = st.DB().Exec(`UPDATE epochs SET finished_at=?`, finish); err != nil {
				t.Fatal(err)
			}
			st.Close()
			session, err := Open(ctx, investigation.OfflineSelection{DB: path, System: "shop", Interval: 30 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			info, err := session.Source.Query(ctx, "GetLensInfo", []byte(`{}`))
			if err != nil {
				t.Fatal(err)
			}
			var clock struct {
				Now string `json:"now"`
			}
			if err = json.Unmarshal(info, &clock); err != nil {
				t.Fatal(err)
			}
			if clock.Now != finish.Format(time.RFC3339Nano) {
				t.Fatalf("invented capture clock: %s", info)
			}
			wantEnd := finish
			if duration == 0 {
				wantEnd = finish.Add(time.Microsecond)
			}
			if !session.QueryReference.Equal(wantEnd) {
				t.Fatalf("derived end %s want %s", session.QueryReference, wantEnd)
			}
			overview, err := session.Source.Query(ctx, "GetOverview", []byte(`{}`))
			if err != nil {
				t.Fatal(err)
			}
			var response struct {
				Range     investigation.Bounds `json:"range"`
				Freshness struct {
					Epoch string `json:"epoch"`
					Age   string `json:"age"`
				} `json:"freshness"`
			}
			if err = json.Unmarshal(overview, &response); err != nil {
				t.Fatal(err)
			}
			if response.Range.End != wantEnd.Format(time.RFC3339Nano) || response.Freshness.Epoch != "9007199254740993" {
				t.Fatalf("default range: %s", overview)
			}
			if duration < time.Second && response.Freshness.Age != "0s" {
				t.Fatalf("tick changed captured freshness: %s", overview)
			}
			var out, diagnostic bytes.Buffer
			args := []string{"query", "GetOverview", "-db", path, "-system", "shop", "-collection-interval", "30s", "-url", "https://lens.example.com/?system=shop&range=1h"}
			if err = investigation.RunWithOffline(ctx, args, strings.NewReader(""), &out, &diagnostic, Open); err != nil {
				t.Fatalf("relative CLI: %v %s", err, out.String())
			}
			var envelope struct {
				Context investigation.Context `json:"context"`
			}
			if err = json.Unmarshal(out.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.Context.Range.End != wantEnd.Format(time.RFC3339Nano) {
				t.Fatalf("relative end: %s", out.String())
			}
			if duration == 0 && !strings.Contains(out.String(), "exclusive_end_inclusion") {
				t.Fatalf("undisclosed adjustment: %s", out.String())
			}
			replay, _ := json.Marshal(envelope.Context)
			out.Reset()
			replayArgs := []string{"query", "GetOverview", "-db", path, "-system", "shop", "-collection-interval", "30s", "-context", "-"}
			if err = investigation.RunWithOffline(ctx, replayArgs, bytes.NewReader(replay), &out, &diagnostic, Open); err != nil {
				t.Fatalf("replay: %v %s", err, out.String())
			}
			if err = json.Unmarshal(out.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.Context.Range.End != wantEnd.Format(time.RFC3339Nano) {
				t.Fatalf("replay applied tick twice: %s", out.String())
			}

			out.Reset()
			args = append(args, "-to", at.Format(time.RFC3339Nano))
			if err = investigation.RunWithOffline(ctx, args, strings.NewReader(""), &out, &diagnostic, Open); err != nil {
				t.Fatalf("fixed CLI: %v %s", err, out.String())
			}
			if err = json.Unmarshal(out.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.Context.Range.End != at.Format(time.RFC3339Nano) {
				t.Fatalf("fixed end extended: %s", out.String())
			}
			for _, c := range []struct {
				end     time.Time
				include bool
			}{{at, false}, {at.Add(time.Microsecond), true}} {
				req, _ := json.Marshal(map[string]any{"metric": "server.cpu_percent", "entity": map[string]string{"type": "ENTITY_TYPE_SERVER", "key": "nats-0"}, "range": map[string]string{"start": at.Add(-time.Minute).Format(time.RFC3339Nano), "end": c.end.Format(time.RFC3339Nano)}})
				series, err := session.Source.Query(ctx, "GetTimeSeries", req)
				if err != nil {
					t.Fatal(err)
				}
				var result struct {
					Series struct {
						Points []json.RawMessage `json:"points"`
					} `json:"series"`
				}
				if err = json.Unmarshal(series, &result); err != nil {
					t.Fatal(err)
				}
				points := len(result.Series.Points)
				if (points > 0) != c.include {
					t.Fatalf("explicit half-open series end %s: %s", c.end, series)
				}
			}
		})
	}
}
