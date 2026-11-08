package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func sqlFixture(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "sample.duckdb")
	db, err := sql.Open("duckdb", p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("CREATE TABLE sample AS SELECT 42 AS answer"); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	p, err = filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSQLJSONMixedRows(t *testing.T) {
	p := sqlFixture(t)
	var out bytes.Buffer
	q := `SELECT 9223372036854775807::BIGINT AS repeated, NULL::BIGINT AS repeated, 18446744073709551615::UBIGINT AS u, 12345678901234567890.1200::DECIMAL(38,4) AS d, [1::BIGINT,NULL] AS a, map([2::BIGINT,1],[NULL,3::BIGINT]) AS m, '\x00\xFF'::BLOB AS b, TIMESTAMP '2026-10-17 14:30:00.123456' AS t, TIMESTAMPTZ '2026-10-17 16:30:00.123456+02' AS z`
	if err := runSQLTo(context.Background(), []string{"-db", p, "-format", "json", q}, &out); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Version   int
		Source    struct{ Kind, Path string }
		Columns   []struct{ Name, Type string }
		Rows      [][]any
		Truncated bool
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Version != 1 || got.Source.Kind != "database" || got.Source.Path != p || len(got.Columns) != 9 || got.Columns[0].Name != "repeated" || got.Columns[1].Name != "repeated" {
		t.Fatalf("metadata: %s", out.String())
	}
	r := got.Rows[0]
	if r[0] != "9223372036854775807" || r[1] != nil || r[2] != "18446744073709551615" || r[3] != "12345678901234567890.12" || r[6] != "AP8=" || r[7] != "2026-10-17T14:30:00.123456Z" || r[8] != "2026-10-17T14:30:00.123456Z" {
		t.Fatalf("row: %#v", r)
	}
	if stringMustJSON(t, r[4]) != `["1",null]` || stringMustJSON(t, r[5]) != `[["2",null],["1","3"]]` {
		t.Fatalf("nested: %#v", r)
	}
}
func stringMustJSON(t *testing.T, v any) string {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}

func TestSQLJSONLimitsAndAtomicity(t *testing.T) {
	p := sqlFixture(t)
	for _, tc := range []struct {
		name     string
		args     []string
		wantErr  bool
		contains string
	}{
		{"empty", []string{"-format", "json", "SELECT answer FROM sample WHERE false"}, false, `"rows":[]`},
		{"truncate", []string{"-format", "json", "-max-rows", "2", "SELECT * FROM range(3)"}, false, `"truncated":true`},
		{"exact", []string{"-format", "json", "-max-rows", "2", "SELECT * FROM range(2)"}, false, `"truncated":false`},
		{"invalid", []string{"-format", "json", "SELECT missing FROM sample"}, true, ""},
		{"bytes", []string{"-format", "json", "-max-output-bytes", "1024", "SELECT repeat('x',2000)"}, true, ""},
		{"timeout", []string{"-format", "json", "-timeout", "1ms", "SELECT sum(a.range*b.range) FROM range(1000000) a,range(1000000) b"}, true, ""},
		{"budget", []string{"-max-rows", "0", "SELECT 1"}, true, ""},
		{"source conflict", []string{"-data-dir", t.TempDir(), "SELECT 1"}, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			err := runSQLTo(context.Background(), append([]string{"-db", p}, tc.args...), &out)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v output=%s", err, &out)
			}
			if tc.wantErr && out.Len() != 0 {
				t.Fatalf("partial output: %s", &out)
			}
			if !strings.Contains(out.String(), tc.contains) {
				t.Fatalf("output=%s", &out)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out bytes.Buffer
	if err := runSQLTo(ctx, []string{"-db", p, "-format", "json", "SELECT 1"}, &out); err == nil || out.Len() != 0 {
		t.Fatalf("cancel: %v %s", err, &out)
	}
	dest := filepath.Join(t.TempDir(), "result.json")
	if err := runSQLTo(context.Background(), []string{"-db", p, "-format", "json", "-output", dest, "SELECT 1"}, &out); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(dest)
	if err != nil || !json.Valid(b) || out.Len() != 0 {
		t.Fatalf("file: %s %v stdout=%s", b, err, &out)
	}
	if err := runSQLTo(context.Background(), []string{"-db", p, "-output", dest, "SELECT 2"}, &out); err == nil {
		t.Fatal("overwrote existing output")
	}
}

// Exercise the actual main error/exit path without starting the collector.
func TestSQLProcessDiagnostics(t *testing.T) {
	if os.Getenv("NATSLENS_SQL_CLI_HELPER") == "1" {
		os.Args = append([]string{"natslens", "sql", "-db", os.Getenv("NATSLENS_SQL_CLI_DB")}, strings.Split(os.Getenv("NATSLENS_SQL_CLI_ARGS"), "\n")...)
		main()
		return
	}
	p := sqlFixture(t)
	for _, args := range []string{"-format\njson\nSELECT private_synthetic_literal FROM sample", "-unknown\nSELECT 1", "-max-rows\n0\nSELECT 1"} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestSQLProcessDiagnostics$")
		cmd.Env = append(os.Environ(), "NATSLENS_SQL_CLI_HELPER=1", "NATSLENS_SQL_CLI_DB="+p, "NATSLENS_SQL_CLI_ARGS="+args)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 || stdout.Len() != 0 || !json.Valid(bytes.TrimSpace(stderr.Bytes())) || strings.Contains(stderr.String(), "private_synthetic_literal") {
			t.Fatalf("exit=%v stdout=%q stderr=%q", err, &stdout, &stderr)
		}
	}
}
