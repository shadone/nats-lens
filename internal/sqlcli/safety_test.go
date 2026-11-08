package sqlcli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	db, e := sql.Open("duckdb", p)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("CREATE TABLE sample AS SELECT 42 AS answer"); e != nil {
		t.Fatal(e)
	}
	if e = db.Close(); e != nil {
		t.Fatal(e)
	}
	p, e = filepath.EvalSymlinks(p)
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func TestReadOnlyNoMigration(t *testing.T) {
	p := fixture(t, "sample.duckdb")
	before, _ := os.ReadFile(p)
	var out bytes.Buffer
	if e := Run(context.Background(), p, "SELECT answer, NULL AS missing FROM sample", &out); e != nil {
		t.Fatal(e)
	}
	if out.String() != "answer  missing\n42      NULL\n(1 rows)\n" {
		t.Fatalf("table=%q", out.String())
	}
	after, _ := os.ReadFile(p)
	if sha256.Sum256(before) != sha256.Sum256(after) {
		t.Fatal("source changed")
	}
	entries, _ := os.ReadDir(filepath.Dir(p))
	if len(entries) != 1 {
		t.Fatalf("sidecars: %v", entries)
	}
}
func TestTrustedSQLExternalCapabilities(t *testing.T) {
	p := fixture(t, "sample.duckdb")
	dir := t.TempDir()
	csv := filepath.Join(dir, "input.csv")
	os.WriteFile(csv, []byte("answer\n42\n"), 0600)
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	for _, q := range []string{
		"SELECT answer FROM read_csv(" + quote(csv) + ")",
		"COPY (SELECT 42 AS answer) TO " + quote(filepath.Join(dir, "output.csv")) + " (HEADER)",
		"ATTACH " + quote(filepath.Join(dir, "attached.duckdb")) + " AS extra (READ_WRITE); CREATE TABLE extra.sample AS SELECT 1; SELECT * FROM extra.sample",
		"SELECT name,value FROM duckdb_settings() WHERE name IN ('enable_external_access','autoinstall_known_extensions','autoload_known_extensions','allow_unsigned_extensions') ORDER BY name",
	} {
		var out bytes.Buffer
		if e := Run(context.Background(), p, q, &out); e != nil {
			t.Fatalf("%s: %v", q, e)
		}
		t.Log(fmt.Sprintf("%s\n%s", q, &out))
	}
	for _, n := range []string{"output.csv", "attached.duckdb"} {
		if _, e := os.Stat(filepath.Join(dir, n)); e != nil {
			t.Fatal(e)
		}
	}
}
