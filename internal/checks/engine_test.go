package checks

import (
	"context"
	"io/fs"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	checkfiles "github.com/shadone/nats-lens/checks"
	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/store"
)

// Every check file parses, and every query binds and runs against the
// schema: an empty epoch gives every check status ok and no rows.
func TestEveryCheckRunsOnEmptyEpoch(t *testing.T) {
	f := newFx(t)
	f.epoch(1, []string{"nats-0"})
	set, err := Load(config.Checks{Timeout: config.Duration(10 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	runs, err := (&Engine{Set: set}).Evaluate(context.Background(), f.st.DB(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != len(set.Checks()) {
		t.Fatalf("%d runs for %d checks", len(runs), len(set.Checks()))
	}
	for _, r := range runs {
		if r.Status != store.RunOK || len(r.Rows) != 0 {
			t.Errorf("%s: %s %q, %d rows", r.Code, r.Status, r.Error, len(r.Rows))
		}
	}
	if len(set.Checks()) < 30 {
		t.Errorf("only %d checks", len(set.Checks()))
	}
}

func TestHeadersHaveRemediationAndUniqueNames(t *testing.T) {
	set, err := Load(config.Checks{})
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, c := range set.Checks() {
		if prev, ok := names[c.Name]; ok {
			t.Errorf("%s and %s share the name %q", prev, c.Code, c.Name)
		}
		names[c.Name] = c.Code
		if len(c.Remediation) < 80 {
			t.Errorf("%s: remediation is too short to be specific: %q", c.Code, c.Remediation)
		}
	}
}

func file(header, sql string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte("/*\n" + header + "\n*/\n" + sql)}
}

const okHeader = `code: stream-901
name: Test
entity: stream
category: health
severity: warning
kind: operational
description: d
remediation: r
thresholds:
  - name: limit
    default: "5"
    unit: count
    description: x`

func TestHeaderValidation(t *testing.T) {
	for name, tc := range map[string]struct {
		header, sql, err string
	}{
		"unknown variable":        {okHeader, `SELECT getvariable('limit'), getvariable('nope')`, `variable "nope"`},
		"unused threshold":        {okHeader, `SELECT 1`, "never reads it"},
		"window without lookback": {okHeader, `SELECT getvariable('limit'), getvariable('window_start')`, "no lookback"},
		"trend without lookback":  {strings.Replace(okHeader, "operational", "trend", 1), `SELECT getvariable('limit')`, "needs a lookback"},
		"code and entity":         {strings.Replace(okHeader, "entity: stream", "entity: consumer", 1), `SELECT getvariable('limit')`, "does not start with"},
		"bad default":             {strings.Replace(okHeader, `"5"`, `"five"`, 1), `SELECT getvariable('limit')`, "default"},
		"unknown header key":      {okHeader + "\nseverity_hint: x", `SELECT getvariable('limit')`, "severity_hint"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := LoadFS(fstest.MapFS{"stream-901.sql": file(tc.header, tc.sql)}, config.Checks{})
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Fatalf("err = %v, want %q", err, tc.err)
			}
		})
	}
	if _, err := LoadFS(fstest.MapFS{"stream-902.sql": file(okHeader, `SELECT getvariable('limit')`)}, config.Checks{}); err == nil ||
		!strings.Contains(err.Error(), "file name") {
		t.Fatalf("file named after another code: %v", err)
	}
}

func TestOverrideValidation(t *testing.T) {
	fsys := fstest.MapFS{"stream-901.sql": file(okHeader, `SELECT getvariable('limit')`)}
	for name, tc := range map[string]struct {
		cfg config.Checks
		err string
	}{
		"unknown disabled code":   {config.Checks{Disabled: []string{"stream-999"}}, `unknown check "stream-999"`},
		"unknown threshold code":  {config.Checks{Thresholds: map[string]map[string]string{"stream-999": {"limit": "1"}}}, `unknown check "stream-999"`},
		"unknown threshold":       {config.Checks{Thresholds: map[string]map[string]string{"stream-901": {"limt": "1"}}}, `unknown threshold "limt"`},
		"bad value":               {config.Checks{Thresholds: map[string]map[string]string{"stream-901": {"limit": "-1"}}}, "not a finite non-negative number"},
		"lookback on operational": {config.Checks{Thresholds: map[string]map[string]string{"stream-901": {"lookback": "1h"}}}, "no lookback"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadFS(fsys, tc.cfg); err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Fatalf("err = %v, want %q", err, tc.err)
			}
		})
	}
	set, err := LoadFS(fsys, config.Checks{Thresholds: map[string]map[string]string{"stream-901": {"limit": "7"}}})
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := set.Value("stream-901", "limit"); v.Number != 7 {
		t.Fatalf("override = %v", v)
	}
}

func TestParseValue(t *testing.T) {
	for _, tc := range []struct {
		unit, in string
		num      float64
		dur      time.Duration
	}{
		{UnitBytes, "4MiB", 4 << 20, 0}, {UnitBytes, "1.5 GiB", 1.5 * (1 << 30), 0}, {UnitBytes, "100", 100, 0},
		{UnitDuration, "2d", 0, 48 * time.Hour}, {UnitDuration, "100ms", 0, 100 * time.Millisecond},
		{UnitRatio, "0.75", 0.75, 0}, {UnitCount, "3", 3, 0},
	} {
		v, err := parseValue(tc.unit, tc.in)
		if err != nil || v.Number != tc.num || v.Duration != tc.dur {
			t.Errorf("parseValue(%s, %q) = %+v, %v", tc.unit, tc.in, v, err)
		}
	}
	if _, err := parseValue(UnitRatio, "1.2"); err == nil {
		t.Error("ratio above 1 accepted")
	}
}

// engineWith returns an engine over the given check files.
func engineWith(t *testing.T, cfg config.Checks, files map[string]string) *Engine {
	t.Helper()
	fsys := fstest.MapFS{}
	for code, sql := range files {
		header := strings.ReplaceAll(okHeader, "stream-901", code)
		header = strings.Split(header, "\nthresholds:")[0]
		fsys[code+".sql"] = file(header, sql)
	}
	set, err := LoadFS(fsys, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return &Engine{Set: set}
}

// A failing check and a check that runs too long become runs with status
// error and one system-001 finding; the others still apply, and an earlier
// finding of the failing check stays firing.
func TestFailingChecksBecomeSystemFinding(t *testing.T) {
	ctx := context.Background()
	f := newFx(t)
	f.epoch(1, []string{"nats-0"})
	f.epoch(2, []string{"nats-0"})
	good := map[string]string{
		"stream-901": `SELECT 'A/x' AS entity_key, 'bad' AS detail`,
		"stream-902": `SELECT 'A/y' AS entity_key, 'bad' AS detail`,
		"stream-903": `SELECT 'A/z' AS entity_key, 'bad' AS detail`,
	}
	e := engineWith(t, config.Checks{}, good)
	if _, err := e.Run(ctx, f.st, 1); err != nil {
		t.Fatal(err)
	}
	broken := map[string]string{
		"stream-901": `SELECT 'A/x' AS entity_key, 'bad' AS detail`,
		"stream-902": `SELECT entity_key FROM no_such_table`,
		"stream-903": `SELECT 'A/z' AS entity_key, 'bad' AS detail FROM range(1000000000) a, range(1000000000) b WHERE a.range = b.range + 7`,
	}
	e = engineWith(t, config.Checks{Timeout: config.Duration(300 * time.Millisecond)}, broken)
	start := time.Now()
	if _, err := e.Run(ctx, f.st, 2); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > 10*time.Second {
		t.Fatalf("the slow check was not cut off: %s", took)
	}
	sys, err := f.st.Finding(ctx, "system-001:", t0)
	if err != nil {
		t.Fatal(err)
	}
	if !sys.Firing() || !strings.Contains(sys.Detail, "2 checks failed") || !strings.Contains(sys.Detail, "stream-902") ||
		!strings.Contains(sys.Detail, "stream-903 (no result within 300ms)") {
		t.Fatalf("system-001 = %+v", sys)
	}
	for _, id := range []string{"stream-901:A/x", "stream-902:A/y", "stream-903:A/z"} {
		if fd, err := f.st.Finding(ctx, id, t0); err != nil || !fd.Firing() {
			t.Errorf("%s after a failed run: %+v %v", id, fd, err)
		}
	}
}

func TestResultValidation(t *testing.T) {
	f := newFx(t)
	f.epoch(1, []string{"nats-0"})
	for name, tc := range map[string]struct{ sql, err string }{
		"missing detail":   {`SELECT 'A/x' AS entity_key`, "no detail column"},
		"unknown column":   {`SELECT 'A/x' AS entity_key, 'd' AS detail, 1 AS extra`, `unexpected result column "extra"`},
		"bad key shape":    {`SELECT 'x' AS entity_key, 'd' AS detail`, "2 parts"},
		"bad severity":     {`SELECT 'A/x' AS entity_key, 'd' AS detail, 'fatal' AS severity`, `severity "fatal"`},
		"duplicate entity": {`SELECT 'A/x' AS entity_key, 'd' AS detail FROM range(2)`, "returned twice"},
	} {
		t.Run(name, func(t *testing.T) {
			e := engineWith(t, config.Checks{}, map[string]string{"stream-901": tc.sql})
			runs, err := e.Evaluate(context.Background(), f.st.DB(), 1)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range runs {
				if r.Code == "stream-901" && (r.Status != store.RunError || !strings.Contains(r.Error, tc.err)) {
					t.Fatalf("run = %s %q, want error %q", r.Status, r.Error, tc.err)
				}
			}
		})
	}
}

// A disabled check does not run, is recorded as disabled and its findings
// resolve.
func TestDisabledCheck(t *testing.T) {
	ctx := context.Background()
	f := newFx(t)
	f.epoch(1, []string{"nats-0"})
	f.epoch(2, []string{"nats-0"})
	files := map[string]string{"stream-901": `SELECT 'A/x' AS entity_key, 'bad' AS detail, 0.5 AS value, json_object('k', 'v') AS related_config`}
	runs, err := engineWith(t, config.Checks{}, files).Evaluate(ctx, f.st.DB(), 1)
	if err != nil || runs[1].Status != store.RunOK {
		t.Fatalf("runs = %+v, %v", runs, err)
	}
	if _, err := engineWith(t, config.Checks{}, files).Run(ctx, f.st, 1); err != nil {
		t.Fatal(err)
	}
	fd, err := f.st.Finding(ctx, "stream-901:A/x", t0)
	if err != nil {
		t.Fatal(err)
	}
	if fd.Value == nil || *fd.Value != 0.5 || fd.RelatedConfig["k"] != "v" {
		t.Fatalf("finding = %+v", fd)
	}
	if _, err := engineWith(t, config.Checks{Disabled: []string{"stream-901"}}, files).Run(ctx, f.st, 2); err != nil {
		t.Fatal(err)
	}
	fd, _ = f.st.Finding(ctx, "stream-901:A/x", t0)
	if fd.Firing() {
		t.Fatal("finding of a disabled check still fires")
	}
	stored, _ := f.st.CheckRuns(ctx, t0.Add(-time.Hour), t0.Add(time.Hour))
	disabled := 0
	for _, r := range stored {
		if r.Epoch == 2 && r.CheckCode == "stream-901" && r.Status == store.RunDisabled {
			disabled++
		}
	}
	if disabled != 1 {
		t.Fatalf("runs = %+v", stored)
	}
}

// A cancelled context stores nothing.
func TestCancelledRunStoresNothing(t *testing.T) {
	f := newFx(t)
	f.epoch(1, []string{"nats-0"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e := engineWith(t, config.Checks{}, map[string]string{"stream-901": `SELECT 'A/x' AS entity_key, 'bad' AS detail`})
	if _, err := e.Run(ctx, f.st, 1); err == nil {
		t.Fatal("run with a cancelled context succeeded")
	}
	var n int
	if err := f.st.DB().QueryRow(`SELECT count(*) FROM check_runs`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("check runs stored: %d %v", n, err)
	}
}

func TestNsText(t *testing.T) {
	f := newFx(t)
	ctx := context.Background()
	conn, err := f.st.DB().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, Prelude); err != nil {
		t.Fatal(err)
	}
	for ns, want := range map[int64]string{0: "0", 7 * 24 * 3600e9: "7d", 2 * 3600e9: "2h", 90e9: "90s", 30e9: "30s", 1500000: "1.5ms", 100e6: "100ms"} {
		var got string
		if err := conn.QueryRowContext(ctx, `SELECT ns_text(?::BIGINT)`, ns).Scan(&got); err != nil || got != want {
			t.Errorf("ns_text(%d) = %q, %v; want %q", ns, got, err, want)
		}
	}
}

// Checks that would start after the budget are recorded as errors without
// Checks that would start after the budget are recorded as errors without
// running: one slow check uses it up, the checks after it are skipped.
func TestBudget(t *testing.T) {
	f := newFx(t)
	f.epoch(1, []string{"nats-0"})
	slow := `SELECT 'A/z' AS entity_key, 'bad' AS detail FROM range(1000000000) a, range(1000000000) b WHERE a.range = b.range + 7`
	e := engineWith(t, config.Checks{Timeout: config.Duration(10 * time.Second)}, map[string]string{
		"stream-901": slow, "stream-902": slow, "stream-903": `SELECT 'A/y' AS entity_key, 'bad' AS detail`,
	})
	e.Budget = 300 * time.Millisecond
	start := time.Now()
	runs, err := e.Evaluate(context.Background(), f.st.DB(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("budget not kept: %s", took)
	}
	timedOut, skipped := 0, 0
	for _, r := range runs {
		switch {
		case strings.Contains(r.Error, "no result within"):
			timedOut++
		case strings.Contains(r.Error, "checks of this collection used up their 300ms"):
			skipped++
		case r.Code == CheckFailedCode:
			if len(r.Rows) != 1 {
				t.Errorf("system-001 = %+v", r)
			}
		}
	}
	if timedOut != 1 || skipped < 1 {
		t.Fatalf("timed out %d, skipped %d: %+v", timedOut, skipped, runs)
	}
}

func TestBudgetRotates(t *testing.T) {
	f := newFx(t)
	f.epoch(1, []string{"nats-0"})
	f.epoch(2, []string{"nats-0"})
	slow := `SELECT 'A/z' AS entity_key, 'bad' AS detail FROM range(1000000000) a, range(1000000000) b WHERE a.range = b.range + 7`
	e := engineWith(t, config.Checks{Timeout: config.Duration(10 * time.Second)}, map[string]string{
		"stream-901": slow, "stream-902": slow, "stream-903": slow,
	})
	e.Budget = 200 * time.Millisecond
	ran := func(epoch int64) string {
		runs, err := e.Evaluate(context.Background(), f.st.DB(), epoch)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range runs {
			if strings.Contains(r.Error, "no result within") {
				return r.Code
			}
		}
		return ""
	}
	first, second := ran(1), ran(2)
	if first == "" || second == "" || first == second {
		t.Fatalf("the check that got the budget: epoch 1 %q, epoch 2 %q", first, second)
	}
}

// A check that has failed in ten epochs in a row makes system-001 critical.
func TestRepeatedFailureEscalates(t *testing.T) {
	ctx := context.Background()
	f := newFx(t)
	e := engineWith(t, config.Checks{}, map[string]string{"stream-901": `SELECT entity_key FROM no_such_table`})
	for n := int64(1); n <= 10; n++ {
		f.epoch(n, []string{"nats-0"})
		if _, err := e.Run(ctx, f.st, n); err != nil {
			t.Fatal(err)
		}
		sys, err := f.st.Finding(ctx, "system-001:", at(n))
		if err != nil {
			t.Fatal(err)
		}
		want := "warning"
		if n == 10 {
			want = "critical"
		}
		if sys.Severity != want {
			t.Fatalf("epoch %d: system-001 %s, want %s (%s)", n, sys.Severity, want, sys.Detail)
		}
	}
}

// A check that returns a value says what it measures.
func TestChecksWithAValueNameIt(t *testing.T) {
	set, err := Load(config.Checks{})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range set.Checks() {
		if regexp.MustCompile(`(?i)\bAS value\b`).MatchString(c.SQL) && (c.ValueName == "" || c.ValueBasis == "") {
			t.Errorf("%s returns a value without value_name and value_basis", c.Code)
		}
	}
}

// A threshold must be a finite number: NaN or Inf would make every
// comparison false and silence the check.
func TestNonFiniteThresholdRejected(t *testing.T) {
	for _, v := range []string{"NaN", "nan", "Inf", "+Inf", "-Inf", "infinity", "0x10", "1_000"} {
		cfg := config.Checks{Thresholds: map[string]map[string]string{"consumer-003": {"warn_ratio": v}}}
		if _, err := Load(cfg); err == nil {
			t.Errorf("warn_ratio %q accepted", v)
		}
		cfg = config.Checks{Thresholds: map[string]map[string]string{"server-008": {"min_bytes": v + "MiB"}}}
		if _, err := Load(cfg); err == nil {
			t.Errorf("min_bytes %qMiB accepted", v)
		}
	}
}

// A check that names another in suppressed_by returns nothing for an
// entity the other fires for in the same epoch.
func TestSuppressedBy(t *testing.T) {
	f := newFx(t)
	f.epoch(1, []string{"nats-0"})
	fsys := fstest.MapFS{
		"stream-901.sql": file(strings.Split(strings.ReplaceAll(okHeader, "stream-901", "stream-901"), "\nthresholds:")[0],
			`SELECT unnest(['A/x', 'A/y']) AS entity_key, 'quorum lost' AS detail`),
		"stream-902.sql": file(strings.Split(strings.ReplaceAll(okHeader, "stream-901", "stream-902"), "\nthresholds:")[0]+
			"\nsuppressed_by: [stream-901]", `SELECT unnest(['A/x', 'A/z']) AS entity_key, 'peers differ' AS detail`),
	}
	set, err := LoadFS(fsys, config.Checks{})
	if err != nil {
		t.Fatal(err)
	}
	runs, err := (&Engine{Set: set}).Evaluate(context.Background(), f.st.DB(), 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range runs {
		if r.Code == "stream-902" && (len(r.Rows) != 1 || r.Rows[0].EntityKey != "A/z") {
			t.Fatalf("stream-902 rows = %+v, want A/z only", r.Rows)
		}
	}
	bad := fstest.MapFS{"stream-902.sql": fsys["stream-902.sql"]}
	if _, err := LoadFS(bad, config.Checks{}); err == nil || !strings.Contains(err.Error(), "suppressed_by") {
		t.Fatalf("unknown suppressor accepted: %v", err)
	}
}

// The remediation for a missing durable warns against restarting the
// owning service, which can replay the stream, and links the playbook.
func TestMissingDurableRemediation(t *testing.T) {
	set, err := Load(config.Checks{})
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"consumer-010", "consumer-013"} {
		c, _ := set.Lookup(code)
		r := strings.Join(strings.Fields(c.Remediation), " ")
		for _, s := range []string{"Do not restart the owning service as the first move", "by_start_time",
			"a consumer whose processing is idempotent", "may be recreated with deliver_policy all"} {
			if !strings.Contains(r, s) {
				t.Errorf("%s remediation lacks %q", code, s)
			}
		}
		src, err := fs.ReadFile(checkfiles.FS, code+".sql")
		if err != nil {
			t.Fatal(err)
		}
		header, _, _ := strings.Cut(string(src), "*/")
		for _, line := range strings.Split(header, "\n") {
			if len(line) > 120 {
				t.Errorf("%s has a header line of %d characters", code, len(line))
			}
		}
		if strings.Contains(r, "restarting the service recreates") || strings.Contains(r, "or restart the service") {
			t.Errorf("%s remediation still suggests a restart: %s", code, r)
		}
	}
}

// Clients behind a load balancer have a longer round trip: the round trip
// check's remediation says what is normal there.
func TestClientRTTRemediationMentionsLoadBalancer(t *testing.T) {
	set, err := Load(config.Checks{})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := set.Lookup("client-001")
	r := strings.Join(strings.Fields(c.Remediation), " ")
	if !strings.Contains(r, "A client that reaches NATS through a load balancer adds a few milliseconds") {
		t.Errorf("client-001 remediation: %s", r)
	}
}

// With less than minQueryTime of budget left a check is not started, so it
// reads "not run" instead of "no result within 30ms".
func TestBudgetTooSmallToStart(t *testing.T) {
	f := newFx(t)
	f.epoch(1, []string{"nats-0"})
	e := engineWith(t, config.Checks{Timeout: config.Duration(10 * time.Second)}, map[string]string{
		"stream-901": `SELECT 'A/z' AS entity_key, 'bad' AS detail FROM range(1000000000) a, range(1000000000) b WHERE a.range = b.range + 7`,
	})
	e.Budget = 30 * time.Millisecond
	runs, err := e.Evaluate(context.Background(), f.st.DB(), 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range runs {
		if r.Code == "stream-901" && !strings.HasPrefix(r.Error, "not run") {
			t.Fatalf("stream-901: %s %q", r.Status, r.Error)
		}
	}
}
