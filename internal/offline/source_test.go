package offline

import (
	"context"
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
