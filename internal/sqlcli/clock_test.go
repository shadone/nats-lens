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
	queries := []string{exact, `SELECT TIME '00:00:00'`, `SELECT NULL::TIME`, `SELECT TIME '12:30:00' WHERE false`}
	for _, typ := range []string{"TIME", "TIMETZ", "TIME_NS"} {
		literal := "'24:00:00'::" + typ
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
