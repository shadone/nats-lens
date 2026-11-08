package sqlcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestJSONRejectsLossyTimeFamilies(t *testing.T) {
	p := fixture(t, "clock.duckdb")
	o := DefaultOptions()
	o.Format = "json"
	// Exact R2 probe: DuckDB distinguishes the times, while the driver has already
	// collapsed 24:00 to 00:00 before JSON value validation can inspect the value.
	exact := `SELECT TIME '24:00:00' = TIME '00:00:00', TIME '24:00:00', TIME '00:00:00', [TIME '24:00:00']`
	queries := []string{exact, `SELECT TIME_NS '24:00:00'`, `SELECT [TIME_NS '24:00:00']`, `SELECT union_value(number := 1)::UNION(number INTEGER, clock TIME)`, `SELECT TIME '00:00:00'`, `SELECT NULL::TIME`, `SELECT TIME '12:30:00' WHERE false`}
	for _, typ := range []string{"TIME", "TIMETZ", "TIME_NS"} {
		literal := "'24:00:00'::" + typ
		if typ == "TIME_NS" {
			literal = "'12:00:00.123456789'::TIME_NS"
		}
		if typ == "TIMETZ" {
			literal = "'24:00:00+02'::TIMETZ"
		}
		for _, expr := range []string{literal, "[" + literal + "]", "[" + literal + "]::" + typ + "[1]", "{'clock':" + literal + "}", "map([1],[" + literal + "])", "map([" + literal + "],[1])", "union_value(clock := " + literal + ")"} {
			queries = append(queries, "SELECT "+expr)
		}
	}
	for _, q := range queries {
		t.Run(q, func(t *testing.T) {
			b, e := Execute(context.Background(), p, q, o)
			var d *Diagnostic
			if !errors.As(e, &d) || d.ErrorDetail.Code != "unsupported_type" || len(b) != 0 || !bytes.Contains([]byte(e.Error()), []byte("VARCHAR")) {
				t.Fatalf("lossy or uninspectable clock result accepted without cast guidance: %s %v", b, e)
			}
		})
	}
}

func TestSQLClockExplicitCastProof(t *testing.T) {
	p := fixture(t, "clock.duckdb")
	o := DefaultOptions()
	o.Format = "json"
	// The SQL engine performs the comparisons and casts before the driver's
	// conversions. This records the distinctions that must survive inspection.
	q := `SELECT TIME '24:00:00' = TIME '00:00:00', CAST(TIME '24:00:00' AS VARCHAR), CAST(TIME '00:00:00' AS VARCHAR), [CAST(TIME '24:00:00' AS VARCHAR)], TIMETZ '24:00:00+02' = TIMETZ '00:00:00+02', CAST(TIMETZ '24:00:00+02' AS VARCHAR), CAST(TIMETZ '00:00:00+02' AS VARCHAR), TIME_NS '12:00:00.123456789' = TIME_NS '12:00:00.123456788', CAST(TIME_NS '12:00:00.123456789' AS VARCHAR)`
	b, e := Execute(context.Background(), p, q, o)
	if e != nil {
		t.Fatal(e)
	}
	var r result
	if e = json.Unmarshal(b, &r); e != nil {
		t.Fatal(e)
	}
	want := `[false,"24:00:00","00:00:00",["24:00:00"],false,"24:00:00+02","00:00:00+02",false,"12:00:00.123456789"]`
	if len(r.Rows) != 1 || string(r.Rows[0]) != want {
		t.Fatalf("clock engine proof: %s", b)
	}
}

// Audit the adjacent conversions without changing their semantics: the driver
// copies date fields, uses integer epoch units for timestamps, and retains the
// three independent interval components. SQL computes comparisons before scan.
func TestSQLTemporalConversionControls(t *testing.T) {
	p := fixture(t, "temporal-controls.duckdb")
	o := DefaultOptions()
	o.Format = "json"
	for _, tc := range []struct{ q, want string }{
		{`SELECT DATE '2026-10-17'=DATE '2026-10-18', DATE '2026-10-17', DATE '2026-10-18'`, `[false,"2026-10-17T00:00:00Z","2026-10-18T00:00:00Z"]`},
		{`SELECT TIMESTAMP '2026-10-17 14:30:00.123456'=TIMESTAMP '2026-10-17 14:30:00.123455', TIMESTAMP '2026-10-17 14:30:00.123456', TIMESTAMP '2026-10-17 14:30:00.123455'`, `[false,"2026-10-17T14:30:00.123456Z","2026-10-17T14:30:00.123455Z"]`},
		{`SELECT TIMESTAMP_S '2026-10-17 14:30:00'=TIMESTAMP_S '2026-10-17 14:30:01', TIMESTAMP_S '2026-10-17 14:30:00', TIMESTAMP_S '2026-10-17 14:30:01'`, `[false,"2026-10-17T14:30:00Z","2026-10-17T14:30:01Z"]`},
		{`SELECT TIMESTAMP_MS '2026-10-17 14:30:00.123'=TIMESTAMP_MS '2026-10-17 14:30:00.124', TIMESTAMP_MS '2026-10-17 14:30:00.123', TIMESTAMP_MS '2026-10-17 14:30:00.124'`, `[false,"2026-10-17T14:30:00.123Z","2026-10-17T14:30:00.124Z"]`},
		{`SELECT TIMESTAMP_NS '2026-10-17 14:30:00.123456789'=TIMESTAMP_NS '2026-10-17 14:30:00.123456788', TIMESTAMP_NS '2026-10-17 14:30:00.123456789', TIMESTAMP_NS '2026-10-17 14:30:00.123456788'`, `[false,"2026-10-17T14:30:00.123456789Z","2026-10-17T14:30:00.123456788Z"]`},
		{`SELECT TIMESTAMPTZ '2026-10-17 16:30:00.123456+02'=TIMESTAMPTZ '2026-10-17 14:30:00.123456+00', TIMESTAMPTZ '2026-10-17 16:30:00.123456+02', TIMESTAMPTZ '2026-10-17 14:30:00.123456+00'`, `[true,"2026-10-17T14:30:00.123456Z","2026-10-17T14:30:00.123456Z"]`},
		{`SELECT INTERVAL '1 month'=INTERVAL '30 days', INTERVAL '1 month', INTERVAL '30 days', to_microseconds(9007199254740993), INTERVAL '-1 months 2 days -3 microseconds'`, `[true,{"days":"0","micros":"0","months":"1"},{"days":"30","micros":"0","months":"0"},{"days":"0","micros":"9007199254740993","months":"0"},{"days":"2","micros":"-3","months":"-1"}]`},
	} {
		b, e := Execute(context.Background(), p, tc.q, o)
		if e != nil {
			t.Fatalf("control %s: %v", tc.q, e)
		}
		var r result
		if e = json.Unmarshal(b, &r); e != nil || len(r.Rows) != 1 || string(r.Rows[0]) != tc.want {
			t.Fatalf("conversion control %s: %s %v", tc.q, b, e)
		}
	}
	// Boundaries and fractional precision are evaluated natively, before casts.
	b, e := Execute(context.Background(), p, `SELECT try_cast('24:00:00' AS TIME_NS) IS NULL, CAST(TIME_NS '23:59:59.999999999' AS VARCHAR), CAST(TIMETZ '00:00:00-15:59:59' AS VARCHAR), CAST(TIMETZ '24:00:00+15:59:59' AS VARCHAR), CAST(TIME_NS '24:00:00' AS VARCHAR)`, o)
	if e != nil {
		t.Fatal(e)
	}
	var boundary result
	if e = json.Unmarshal(b, &boundary); e != nil || len(boundary.Rows) != 1 || string(boundary.Rows[0]) != `[false,"23:59:59.999999999","00:00:00-15:59:59","24:00:00+15:59:59","24:00:00"]` {
		t.Fatalf("native clock boundary proof: %s %v", b, e)
	}
}
