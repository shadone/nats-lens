package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shadone/nats-lens/internal/store"
)

func TestOfflineCLIHelperProcess(t *testing.T) {
	if os.Getenv("NATSLENS_TEST_OFFLINE_CLI") != "1" {
		return
	}
	args := os.Args
	for i, a := range args {
		if a == "--" {
			if err := run(context.Background(), args[i+1:]); err != nil {
				os.Exit(1)
			}
			os.Exit(0)
		}
	}
	os.Exit(2)
}

// Each invocation executes the real CLI dispatcher in a separate OS process.
// No in-memory provider or traversal cache can survive between these calls.
func TestOfflineCLITwoProcessesContinue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "closed.duckdb")
	st, err := store.OpenPath(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO epochs (epoch,started_at,finished_at,duration_ms,expected_servers,observed_servers,expected_names,observed_names,missing_names,endpoint_failures,partial,errors) VALUES (1,TIMESTAMP '2026-10-17 14:29:00',TIMESTAMP '2026-10-17 14:29:01',1000,0,0,[],[],[],0,false,[])`,
		`INSERT INTO streams(account,stream,first_epoch,last_epoch) VALUES ('APP','orders',1,1),('APP','payments',1,1)`,
		`INSERT INTO stream_config(server_id,account,stream,first_epoch,last_epoch,config_hash,replicas) VALUES ('S0','APP','orders',1,1,'generic',1),('S0','APP','payments',1,1,'generic',1)`,
		`INSERT INTO stream_stats(epoch,server_id,account,stream,messages,bytes,is_leader) VALUES (1,'S0','APP','orders',1,10,true),(1,'S0','APP','payments',1,20,true)`,
	} {
		if _, err = st.DB().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err = st.Close(); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	invoke := func(token string) ([]byte, error) {
		args := []string{"-test.run=^TestOfflineCLIHelperProcess$", "--", "investigate", "query", "ListStreams", "-db", path, "-system", "shop", "-collection-interval", "30s", "-page-size", "1"}
		if token != "" {
			args = append(args, "-page-token", token)
		}
		cmd := exec.Command(executable, args...)
		cmd.Env = append(os.Environ(), "NATSLENS_TEST_OFFLINE_CLI=1")
		return cmd.CombinedOutput()
	}
	first, err := invoke("")
	if err != nil {
		t.Fatalf("first: %v %s", err, first)
	}
	var result struct {
		Response struct {
			NextPageToken string `json:"nextPageToken"`
		} `json:"response"`
	}
	if err = json.Unmarshal(first, &result); err != nil {
		t.Fatal(err)
	}
	if result.Response.NextPageToken == "" {
		t.Fatalf("fixture must page: %s", first)
	}
	second, err := invoke(result.Response.NextPageToken)
	if err != nil {
		t.Fatalf("second process cannot continue: %v %s", err, second)
	}
	if !strings.Contains(string(second), `"name": "payments"`) {
		t.Fatalf("wrong second page: %s", second)
	}
}

func TestOfflineOccurrenceCLITwoProcessesContinue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "closed.duckdb")
	st, err := store.OpenPath(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO epochs (epoch,started_at,finished_at,duration_ms,expected_servers,observed_servers,expected_names,observed_names,missing_names,endpoint_failures,partial,errors) SELECT n, TIMESTAMP '2026-10-17 14:23:00'+n*INTERVAL 1 MINUTE,TIMESTAMP '2026-10-17 14:23:01'+n*INTERVAL 1 MINUTE,1000,0,0,[],[],[],0,false,[] FROM range(1,4) t(n)`,
		`INSERT INTO finding_transitions(finding_id,check_code,entity_type,entity_key,epoch,time,kind,severity,detail) SELECT 'server-001:nats-0','server-001','server','nats-0',n,TIMESTAMP '2026-10-17 14:23:00'+n*INTERVAL 1 MINUTE,CASE WHEN n=2 THEN 'resolved' ELSE 'firing' END,'warning','Generic recorded occurrence' FROM range(1,4) t(n)`,
	} {
		if _, err = st.DB().Exec(q); err != nil {
			st.Close()
			t.Fatal(err)
		}
	}
	if err = st.Close(); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	invoke := func(token string) []byte {
		t.Helper()
		args := []string{"-test.run=^TestOfflineCLIHelperProcess$", "--", "investigate", "query", "ListFindingEpisodes", "-db", path, "-system", "shop", "-collection-interval", "30s", "-page-size", "1"}
		if token != "" {
			args = append(args, "-page-token", token)
		}
		cmd := exec.Command(executable, args...)
		cmd.Env = append(os.Environ(), "NATSLENS_TEST_OFFLINE_CLI=1")
		out, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatalf("occurrence CLI process: %v %s", e, out)
		}
		return out
	}
	type output struct {
		Response struct {
			Next     string `json:"nextPageToken"`
			Episodes []struct {
				First string `json:"firstRetainedEpoch"`
			} `json:"episodes"`
		} `json:"response"`
	}
	var a, b output
	if err = json.Unmarshal(invoke(""), &a); err != nil {
		t.Fatal(err)
	}
	if a.Response.Next == "" || len(a.Response.Episodes) != 1 || a.Response.Episodes[0].First != "3" {
		t.Fatalf("first occurrence: %+v", a)
	}
	if err = json.Unmarshal(invoke(a.Response.Next), &b); err != nil {
		t.Fatal(err)
	}
	if len(b.Response.Episodes) != 1 || b.Response.Episodes[0].First != "1" || b.Response.Next != "" {
		t.Fatalf("reopened occurrence traversal: %+v", b)
	}
}
