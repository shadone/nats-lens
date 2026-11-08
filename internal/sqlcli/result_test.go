package sqlcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"database/sql"
	duckdb "github.com/duckdb/duckdb-go/v2"
)

func TestJSONValue(t *testing.T) {
	om := duckdb.OrderedMap{}
	om.Set(int64(2), []any{nil, uint64(math.MaxUint64)})
	for _, tc := range []struct {
		v    any
		want string
	}{
		{nil, `null`}, {true, `true`}, {"", `""`}, {[]byte{}, `""`}, {int8(-1), `"-1"`}, {uint8(255), `"255"`}, {float32(1.25), `1.25`}, {math.Inf(1), `"+Inf"`}, {math.Inf(-1), `"-Inf"`}, {math.NaN(), `"NaN"`},
		{new(big.Int).Lsh(big.NewInt(1), 100), `"1267650600228229401496703205376"`},
		{duckdb.Decimal{Width: 38, Scale: 4, Value: big.NewInt(-1)}, `"-0.0001"`},
		{duckdb.Interval{Months: 2, Days: 3, Micros: 9223372036854775807}, `{"days":"3","micros":"9223372036854775807","months":"2"}`},
		{time.Date(2026, 10, 17, 16, 30, 0, 123456789, time.FixedZone("+02", 7200)), `"2026-10-17T14:30:00.123456789Z"`},
		{om, `[["2",[null,"18446744073709551615"]]]`},
		{map[string]any{"i": int64(9223372036854775807), "n": nil}, `{"i":"9223372036854775807","n":null}`},
		{duckdb.Union{Tag: "number", Value: int64(1)}, `{"tag":"number","value":"1"}`},
		{duckdb.UUID{}, `"00000000-0000-0000-0000-000000000000"`},
	} {
		got, e := jsonValue(tc.v)
		if e != nil {
			t.Fatal(e)
		}
		b, e := json.Marshal(got)
		if e != nil || string(b) != tc.want {
			t.Errorf("%T: %s %v, want %s", tc.v, b, e, tc.want)
		}
	}
	for _, v := range []any{make(chan int), string([]byte{255}), []any{make(chan int)}, map[string]any{"bad": make(chan int)}, map[string]any{string([]byte{255}): 1}} {
		if _, e := jsonValue(v); e == nil {
			t.Errorf("accepted %T", v)
		}
	}
}

func TestExecuteOptionsPathsAndErrors(t *testing.T) {
	p := fixture(t, "shop data ' &+.duckdb")
	o := DefaultOptions()
	o.Format = "json"
	for _, q := range []string{"SELECT 1", "SELECT NULL::VARCHAR AS empty WHERE false", `SELECT {'nested':[1::BIGINT,NULL]} AS s, 1267650600228229401496703205376::HUGEINT AS h, TIMESTAMP_NS '2026-10-17 14:30:00.123456789' AS ns`, "SELECT from_hex('FF')::VARCHAR"} {
		b, e := Execute(context.Background(), p, q, o)
		if e != nil || !json.Valid(b) {
			t.Fatalf("%s: %v %s", q, e, b)
		}
		if strings.Contains(q, "TIMESTAMP_NS") && !bytes.Contains(b, []byte("123456789Z")) {
			t.Fatalf("lost timestamp precision: %s", b)
		}
	}
	for _, change := range []func(*Options){func(o *Options) { o.Format = "xml" }, func(o *Options) { o.MaxRows = 0 }, func(o *Options) { o.MaxRows = 100001 }, func(o *Options) { o.MaxBytes = 1 }, func(o *Options) { o.MaxBytes = 17 << 20 }, func(o *Options) { o.Timeout = 0 }, func(o *Options) { o.Timeout = time.Minute }} {
		bad := o
		change(&bad)
		b, e := Execute(context.Background(), p, "SELECT 1", bad)
		if e == nil || len(b) != 0 {
			t.Fatalf("options accepted: %+v", bad)
		}
	}
	for _, path := range []string{filepath.Join(t.TempDir(), "missing.duckdb"), t.TempDir()} {
		if _, e := SourcePath(path); e == nil {
			t.Fatal("accepted source", path)
		}
	}
	for _, name := range []string{"query?.duckdb", "fragment#.duckdb", "percent%.duckdb"} {
		path := filepath.Join(t.TempDir(), name)
		if e := os.WriteFile(path, []byte("synthetic"), 0600); e != nil {
			t.Fatal(e)
		}
		if _, e := SourcePath(path); e == nil {
			t.Fatal("accepted ambiguous DSN", name)
		}
	}
	link := filepath.Join(t.TempDir(), "alias")
	if e := os.Symlink(p, link); e != nil {
		t.Fatal(e)
	}
	b, e := Execute(context.Background(), link, "SELECT 1", o)
	var linked result
	if e != nil || json.Unmarshal(b, &linked) != nil || linked.Source.Path != p {
		t.Fatalf("symlink: %s %v", b, e)
	}
	corrupt := filepath.Join(t.TempDir(), "corrupt.duckdb")
	os.WriteFile(corrupt, []byte("synthetic invalid database"), 0600)
	for _, tc := range []struct{ path, q, code string }{{p, "DELETE FROM sample", "query_failed"}, {p, "SELECT missing_private_literal FROM sample", "query_failed"}, {corrupt, "SELECT 1", "source_unavailable"}} {
		b, e := Execute(context.Background(), tc.path, tc.q, o)
		var d *Diagnostic
		if !errors.As(e, &d) || d.ErrorDetail.Code != tc.code || len(b) != 0 || strings.Contains(e.Error(), "missing_private_literal") {
			t.Fatalf("diagnostic: %s %v", b, e)
		}
	}
}

func TestExecuteLimitsAndCancel(t *testing.T) {
	p := fixture(t, "sample.duckdb")
	for _, format := range []string{"table", "json"} {
		o := DefaultOptions()
		o.Format = format
		o.MaxBytes = 1024
		for _, q := range []string{"SELECT repeat('x',2000)", "SELECT repeat('x',300) FROM range(10)"} {
			b, e := Execute(context.Background(), p, q, o)
			if e == nil || len(b) > 0 {
				t.Fatalf("byte budget %s: %v %d", format, e, len(b))
			}
		}
		o.MaxBytes = DefaultMaxBytes
		o.MaxRows = 2
		b, e := Execute(context.Background(), p, "SELECT * FROM range(3)", o)
		if e != nil || !bytes.Contains(b, []byte("truncated")) {
			t.Fatalf("truncate: %s %v", b, e)
		}
		ctx, cancel := context.WithCancel(context.Background())
		go func() { time.Sleep(20 * time.Millisecond); cancel() }()
		b, e = Execute(ctx, p, "SELECT sum(a.range*b.range) FROM range(1000000) a, range(1000000) b", o)
		var d *Diagnostic
		if !errors.As(e, &d) || d.ErrorDetail.Code != "cancelled" || len(b) > 0 {
			t.Fatalf("cancel: %s %v", b, e)
		}
	}
}

type failWriter struct{ short bool }

func (w failWriter) Write(b []byte) (int, error) {
	if w.short {
		return len(b) - 1, nil
	}
	return 0, io.ErrClosedPipe
}
func TestPublication(t *testing.T) {
	p := fixture(t, "source.duckdb")
	before, _ := os.ReadFile(p)
	for _, dest := range []string{p, filepath.Join(t.TempDir(), "missing", "output")} {
		if e := Publish(dest, []byte("result")); e == nil {
			t.Fatal("published", dest)
		}
	}
	dir := t.TempDir()
	link := filepath.Join(dir, "symlink")
	os.Symlink(p, link)
	hard := filepath.Join(dir, "hardlink")
	os.Link(p, hard)
	for _, dest := range []string{link, hard} {
		if e := Publish(dest, []byte("result")); e == nil {
			t.Fatal("replaced alias")
		}
	}
	after, _ := os.ReadFile(p)
	if !bytes.Equal(before, after) {
		t.Fatal("source changed")
	}
	dest := filepath.Join(dir, "output")
	var wg sync.WaitGroup
	var mu sync.Mutex
	success := 0
	for range 2 {
		wg.Go(func() {
			if Publish(dest, []byte("complete\n")) == nil {
				mu.Lock()
				success++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if success != 1 {
		t.Fatalf("publish winners %d", success)
	}
	info, e := os.Stat(dest)
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("permissions: %v %v", info, e)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 3 {
		t.Fatalf("temporary files leaked: %v", entries)
	}
	for _, w := range []io.Writer{failWriter{}, failWriter{short: true}} {
		if Write(w, []byte("result")) == nil {
			t.Fatal("missing write error")
		}
	}
	var out bytes.Buffer
	if e := Write(&out, []byte("result")); e != nil || out.String() != "result" {
		t.Fatal(e)
	}
}

func TestReadOnlyLiveFileLock(t *testing.T) {
	if os.Getenv("NATSLENS_SQL_LOCK_HELPER") == "1" {
		db, e := sql.Open("duckdb", os.Getenv("NATSLENS_SQL_LOCK_PATH"))
		if e != nil {
			t.Fatal(e)
		}
		defer db.Close()
		if _, e = db.Exec("INSERT INTO sample VALUES (43)"); e != nil {
			t.Fatal(e)
		}
		os.Stdout.Write([]byte("ready\n"))
		io.Copy(io.Discard, os.Stdin)
		return
	}
	p := fixture(t, "sample.duckdb")
	cmd := exec.Command(os.Args[0], "-test.run=^TestReadOnlyLiveFileLock$")
	cmd.Env = append(os.Environ(), "NATSLENS_SQL_LOCK_HELPER=1", "NATSLENS_SQL_LOCK_PATH="+p)
	stdin, e := cmd.StdinPipe()
	if e != nil {
		t.Fatal(e)
	}
	stdout, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() {
		stdin.Close()
		if e := cmd.Wait(); e != nil {
			t.Error(e)
		}
	}()
	ready := make([]byte, 6)
	if _, e = io.ReadFull(stdout, ready); e != nil {
		t.Fatal(e)
	}
	b, e := Execute(context.Background(), p, "SELECT 1", DefaultOptions())
	var d *Diagnostic
	if !errors.As(e, &d) || d.ErrorDetail.Code != "source_unavailable" || len(b) > 0 {
		t.Fatalf("live lock: %s %v", b, e)
	}
}

func TestReadOnlyPreservesOrphanWAL(t *testing.T) {
	p := fixture(t, "sample.duckdb")
	cmd := exec.Command(os.Args[0], "-test.run=^TestReadOnlyLiveFileLock$")
	cmd.Env = append(os.Environ(), "NATSLENS_SQL_LOCK_HELPER=1", "NATSLENS_SQL_LOCK_PATH="+p)
	stdin, e := cmd.StdinPipe()
	if e != nil {
		t.Fatal(e)
	}
	defer stdin.Close()
	stdout, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	ready := make([]byte, 6)
	if _, e = io.ReadFull(stdout, ready); e != nil {
		t.Fatal(e)
	}
	if e = cmd.Process.Kill(); e != nil {
		t.Fatal(e)
	}
	_ = cmd.Wait()
	beforeDB, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	beforeWAL, e := os.ReadFile(p + ".wal")
	if e != nil {
		t.Fatal(e)
	}
	b, e := Execute(context.Background(), p, "SELECT count(*) FROM sample", DefaultOptions())
	t.Logf("read-only orphan WAL inspection: result=%q error=%v", b, e)
	afterDB, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	afterWAL, e := os.ReadFile(p + ".wal")
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(beforeDB, afterDB) || !bytes.Equal(beforeWAL, afterWAL) {
		t.Fatal("read-only open changed source database or WAL")
	}
	entries, e := os.ReadDir(filepath.Dir(p))
	if e != nil || len(entries) != 2 {
		t.Fatalf("source entries: %v %v", entries, e)
	}
}

func TestJSONRejectsLossyDriverJSONAliases(t *testing.T) {
	p := fixture(t, "sample.duckdb")
	o := DefaultOptions()
	o.Format = "json"
	for _, q := range []string{`SELECT '9223372036854775807'::JSON AS j`, `SELECT ['9223372036854775807'::JSON] AS nested`, `SELECT {'j':'{"same":1,"same":2}'::JSON} AS nested`, `SELECT map([1],['9223372036854775807'::JSON]) AS nested`, `SELECT union_value(j := '9223372036854775807'::JSON)`} {
		b, e := Execute(context.Background(), p, q, o)
		if e == nil || len(b) != 0 {
			t.Errorf("lossy JSON accepted: %s %v", b, e)
		}
	}
	b, e := Execute(context.Background(), p, `SELECT CAST('9223372036854775807'::JSON AS VARCHAR) AS j`, o)
	if e != nil || !bytes.Contains(b, []byte(`"9223372036854775807"`)) {
		t.Fatalf("explicit cast: %s %v", b, e)
	}
}
