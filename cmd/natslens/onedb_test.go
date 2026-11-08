package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shadone/nats-lens/internal/store"
)

// writeTwoSystemDatabases writes a two-system configuration and a database
// for each system whose single epoch expects a server named after it.
func writeTwoSystemDatabases(t *testing.T) (conf string) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	conf = filepath.Join(dir, "natslens.yaml")
	body := "data_dir: " + filepath.Join(dir, "data") + `
systems:
  - id: shop-prod
  - id: shop-staging
`
	if err := os.WriteFile(conf, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"shop-prod", "shop-staging"} {
		st, err := store.Open(ctx, filepath.Join(dir, "data", id))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.DB().Exec(`INSERT INTO epochs VALUES (1, now(), now(), 0, 1, 0, ['` + id + `-0'], [], ['` + id + `-0'], 0, true, [], NULL)`); err != nil {
			t.Fatal(err)
		}
		st.Close()
	}
	return conf
}

// sql and checks run read the database of the system -system names.
func TestOneDatabaseCommandsTakeSystem(t *testing.T) {
	ctx := context.Background()
	conf := writeTwoSystemDatabases(t)
	var out bytes.Buffer
	if err := runSQLTo(ctx, []string{"-config", conf, "-system", "shop-staging", "SELECT expected_names[1] AS e FROM epochs"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "shop-staging-0") || strings.Contains(out.String(), "shop-prod-0") {
		t.Fatalf("sql -system shop-staging:\n%s", out.String())
	}
	out.Reset()
	if err := runChecksCmd(ctx, []string{"run", "-config", conf, "-system", "shop-prod"}, &out); err != nil {
		t.Fatal(err)
	}
	if !regexpLine(out.String(), "server-001", "shop-prod-0") || strings.Contains(out.String(), "shop-staging-0") {
		t.Fatalf("checks run -system shop-prod:\n%s", out.String())
	}
	for _, args := range [][]string{
		{"-config", conf, "SELECT 1"},
		{"-config", conf, "-system", "shop-edge", "SELECT 1"},
	} {
		if err := runSQLTo(ctx, args, &out); err == nil || !strings.Contains(err.Error(), "invalid_argument") || strings.Contains(err.Error(), "shop-prod, shop-staging") {
			t.Errorf("sql %v: err = %v, want a safe invalid_argument diagnostic", args, err)
		}
	}
	if err := runChecksCmd(ctx, []string{"run", "-config", conf}, &out); err == nil || !strings.Contains(err.Error(), "-system") {
		t.Errorf("checks run without -system: err = %v", err)
	}
}

// Without -config, sql reads -data-dir, or ./data, as before.
func TestSQLDataDirStillWorks(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	var out bytes.Buffer
	if err := runSQLTo(ctx, []string{"-data-dir", dir, "SELECT 42 AS answer"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "42") {
		t.Fatalf("output:\n%s", out.String())
	}
}

// once takes -system and refuses an unknown one; run does not take it.
func TestOnceSystemFlag(t *testing.T) {
	conf := twoSystemsConfig(t, t.TempDir(), 0, "nats://127.0.0.1:1", "nats://127.0.0.1:2")
	err := run(context.Background(), []string{"once", "-config", conf, "-system", "shop-edge"})
	if err == nil || !strings.Contains(err.Error(), "shop-prod, shop-staging") {
		t.Fatalf("once -system shop-edge: err = %v", err)
	}
	if err := run(context.Background(), []string{"run", "-config", conf, "-system", "shop-prod"}); err == nil {
		t.Fatal("run accepted -system")
	}
}
