package main

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shadone/nats-lens/internal/store"
)

func TestChecksList(t *testing.T) {
	var out bytes.Buffer
	if err := runChecksCmd(context.Background(), []string{"list"}, &out); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"system-001", "consumer-010", "Expected consumer missing", "max_idle=5m"} {
		if !strings.Contains(out.String(), s) {
			t.Errorf("list lacks %q:\n%s", s, out.String())
		}
	}
	cfgPath := filepath.Join(t.TempDir(), "natslens.yaml")
	if err := os.WriteFile(cfgPath, []byte("checks:\n  disabled: [stream-005]\n  thresholds:\n    consumer-006: {max_idle: 2m}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := runChecksCmd(context.Background(), []string{"list", "-config", cfgPath}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "max_idle=2m") || !regexpLine(out.String(), "stream-005", "disabled") {
		t.Errorf("overrides not shown:\n%s", out.String())
	}
	if err := os.WriteFile(cfgPath, []byte("checks:\n  disabled: [stream-999]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runChecksCmd(context.Background(), []string{"list", "-config", cfgPath}, &out); err == nil ||
		!strings.Contains(err.Error(), "stream-999") {
		t.Fatalf("unknown code accepted: %v", err)
	}
}

func regexpLine(text, a, b string) bool {
	for _, l := range strings.Split(text, "\n") {
		if strings.HasPrefix(l, a) && strings.Contains(l, b) {
			return true
		}
	}
	return false
}

// checks run reads a database read-only and prints the failing entities;
// it stores nothing.
func TestChecksRun(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().Exec(`INSERT INTO epochs VALUES (1, now(), now(), 0, 2, 1, ['nats-0', 'nats-1'], ['nats-0'], ['nats-1'], 0, true, [], NULL)`); err != nil {
		t.Fatal(err)
	}
	st.Close()
	var out bytes.Buffer
	if err := runChecksCmd(ctx, []string{"run", "-data-dir", dir}, &out); err != nil {
		t.Fatal(err)
	}
	if !regexpLine(out.String(), "server-001", "nats-1") || !strings.Contains(out.String(), "nothing stored") {
		t.Fatalf("output:\n%s", out.String())
	}
	st, err = store.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var n int
	if err := st.DB().QueryRow(`SELECT count(*) FROM check_runs`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("check runs stored: %d %v", n, err)
	}
	// A directory without a database is an error, not a new database.
	if err := runChecksCmd(ctx, []string{"run", "-data-dir", t.TempDir()}, &out); err == nil {
		t.Fatal("run without a database succeeded")
	}
}

// checks run reads the database within the system's memory limit and
// spill bound, as the collector does.
func TestChecksRunIsBounded(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().Exec(`INSERT INTO epochs VALUES (1, now(), now(), 0, 1, 1, ['nats-0'], ['nats-0'], [], 0, false, [], NULL)`); err != nil {
		t.Fatal(err)
	}
	st.Close()
	cfgPath := filepath.Join(dir, "natslens.yaml")
	if err := os.WriteFile(cfgPath, []byte("data_dir: "+dir+"\nduckdb_memory_limit: 300MiB\nduckdb_max_temp_size: 700MiB\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	orig := checksDBReady
	t.Cleanup(func() { checksDBReady = orig })
	var mem, spill string
	checksDBReady = func(ctx context.Context, db *sql.DB) {
		if err := db.QueryRowContext(ctx, `SELECT current_setting('memory_limit'), current_setting('max_temp_directory_size')`).Scan(&mem, &spill); err != nil {
			t.Error(err)
		}
	}
	var out bytes.Buffer
	if err := runChecksCmd(ctx, []string{"run", "-config", cfgPath}, &out); err != nil {
		t.Fatal(err)
	}
	if mem != "300.0 MiB" || spill != "700.0 MiB" {
		t.Errorf("memory_limit %q, max_temp_directory_size %q; want 300.0 MiB and 700.0 MiB", mem, spill)
	}
}

// checks list fails when the remediation file names an unknown check.
func TestChecksListRejectsUnknownRemediationCode(t *testing.T) {
	dir := t.TempDir()
	rem := filepath.Join(dir, "remediation.yaml")
	if err := os.WriteFile(rem, []byte("stream-999: Nobody knows this check.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "natslens.yaml")
	if err := os.WriteFile(cfgPath, []byte("checks:\n  remediation_file: "+rem+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := runChecksCmd(context.Background(), []string{"list", "-config", cfgPath}, &out)
	if err == nil || !strings.Contains(err.Error(), `unknown check "stream-999"`) {
		t.Fatalf("unknown remediation code accepted: %v", err)
	}
}
