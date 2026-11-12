package storeprov_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	"github.com/shadone/nats-lens/internal/store"
)

// /metrics reports the bytes the system's DuckDB spilled, read at the
// scrape, and the bound.
func TestMetricsSpill(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	srv, _ := apitest.Serve(t, &storeprov.Provider{St: st, Interval: 30 * time.Second, Live: true, Clock: func() time.Time { return now }})
	body := scrape(t, srv.URL+"/metrics")
	if !regexp.MustCompile(`(?m)^nats_lens_db_spill_bytes\{system="default"\} 0$`).MatchString(body) {
		t.Errorf("no spill of 0 before any query spilled:\n%s", body)
	}
	if regexp.MustCompile(`(?m)^nats_lens_db_spill_limit_bytes\{`).MatchString(body) {
		t.Error("a bound reported before one was set")
	}

	if err := st.SetMemoryLimit(ctx, 64<<20); err != nil {
		t.Fatal(err)
	}
	if err := st.SetMaxTempSize(ctx, 1<<30); err != nil {
		t.Fatal(err)
	}
	// A temporary table larger than the memory limit holds its spill
	// until it is dropped.
	conn, err := st.DB().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `CREATE TEMP TABLE filler AS SELECT i, md5(i::VARCHAR) || md5((i + 1)::VARCHAR) m FROM range(1500000) t(i)`); err != nil {
		t.Fatal(err)
	}
	held, err := st.TempBytes(ctx)
	if err != nil || held < 1<<20 {
		t.Fatalf("the filler spilled %d bytes, %v", held, err)
	}
	body = scrape(t, srv.URL+"/metrics")
	m := regexp.MustCompile(`(?m)^nats_lens_db_spill_bytes\{system="default"\} (\S+)$`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no spill series:\n%s", body)
	}
	if v, err := strconv.ParseFloat(m[1], 64); err != nil || v < 1<<20 {
		t.Errorf("spill %s, want at least 1 MiB (the store holds %d)", m[1], held)
	}
	if !regexp.MustCompile(`(?m)^nats_lens_db_spill_limit_bytes\{system="default"\} 1\.073741824e\+09$`).MatchString(body) {
		t.Errorf("no bound of 1 GiB:\n%s", body)
	}
}

// A failed read of the spill leaves out only the spill: the bound and the
// system's other series stay, and the scrape succeeds.
func TestMetricsSpillReadFails(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.SetMaxTempSize(ctx, 1<<30); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(storeprov.SetSpillBytes(func(*store.Store, context.Context) (int64, error) {
		return 0, errors.New("temporary files not read")
	}))
	var logs bytes.Buffer
	srv, _ := apitest.Serve(t, &storeprov.Provider{St: st, Interval: 30 * time.Second, Live: true, Clock: func() time.Time { return now },
		Log: slog.New(slog.NewTextHandler(&logs, nil))})
	body := scrape(t, srv.URL+"/metrics")
	if regexp.MustCompile(`(?m)^nats_lens_db_spill_bytes\{`).MatchString(body) {
		t.Error("a spill sample after its read failed")
	}
	if n := strings.Count(logs.String(), "spill not read"); n != 1 || !strings.Contains(logs.String(), "level=WARN") {
		t.Errorf("want one warning for the failed read, got %d:\n%s", n, logs.String())
	}
	for _, want := range []string{
		`nats_lens_db_spill_limit_bytes{system="default"} 1.073741824e+09`,
		`nats_lens_system_scrape_success{system="default"} 1`,
		`nats_lens_findings_muted{system="default"} 0`,
	} {
		if !regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(want) + `$`).MatchString(body) {
			t.Errorf("missing %q:\n%s", want, body)
		}
	}
}
