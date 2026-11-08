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
