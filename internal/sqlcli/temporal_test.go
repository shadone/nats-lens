package sqlcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestJSONTemporalValuesRejectUnfaithfulRepresentations(t *testing.T) {
	p := fixture(t, "temporal.duckdb")
	o := DefaultOptions()
	o.Format = "json"
	for _, typ := range []string{"DATE", "TIMESTAMP", "TIMESTAMPTZ", "TIMESTAMP_S", "TIMESTAMP_MS", "TIMESTAMP_NS"} {
		for _, value := range []string{"infinity", "-infinity"} {
			expr := fmt.Sprintf("'%s'::%s", value, typ)
			// DuckDB itself establishes infinity; never infer it from a suspicious date.
			proof, e := Execute(context.Background(), p, "SELECT isfinite("+expr+") AS finite, CAST("+expr+" AS VARCHAR) AS text", o)
			if e != nil || !bytes.Contains(proof, []byte(`[false,"`+value+`"]`)) {
				t.Fatalf("driver proof %s: %s %v", expr, proof, e)
			}
			for _, wrapped := range []string{expr, "[" + expr + "]", "[" + expr + "]::" + typ + "[1]", "{'value':" + expr + "}", "map([1],[" + expr + "])", "map([" + expr + "],[1])", "union_value(value := " + expr + ")"} {
				t.Run(wrapped, func(t *testing.T) {
					b, e := Execute(context.Background(), p, "SELECT "+wrapped, o)
					var d *Diagnostic
					if !errors.As(e, &d) || d.ErrorDetail.Code != "unsupported_value" || len(b) != 0 || !bytes.Contains([]byte(e.Error()), []byte("VARCHAR")) {
						t.Fatalf("unfaithful temporal accepted or guidance missing: %s %v", b, e)
					}
				})
			}
		}
	}
	for _, expr := range []string{`DATE '10000-01-01'`, `DATE '-0001-01-01'`, `TIMESTAMP '10000-01-01'`, `TIMESTAMP '-0001-01-01 00:00:00'`, `TIMESTAMPTZ '10000-01-01 00:00:00+00'`, `TIMESTAMP_S '10000-01-01'`, `TIMESTAMP_MS '10000-01-01'`} {
		proof, e := Execute(context.Background(), p, "SELECT isfinite("+expr+")", o)
		if e != nil || !bytes.Contains(proof, []byte(`[true]`)) {
			t.Fatalf("finite proof %s: %s %v", expr, proof, e)
		}
		b, e := Execute(context.Background(), p, "SELECT "+expr, o)
		var d *Diagnostic
		if !errors.As(e, &d) || d.ErrorDetail.Code != "unsupported_value" || len(b) != 0 {
			t.Errorf("out-of-range accepted %s: %s %v", expr, b, e)
		}
	}
}

func TestJSONTemporalFiniteBoundaries(t *testing.T) {
	p := fixture(t, "temporal.duckdb")
	o := DefaultOptions()
	o.Format = "json"
	for _, expr := range []string{`DATE '0001-01-01'`, `DATE '9999-12-31'`, `TIMESTAMP '0001-01-01'`, `TIMESTAMP '9999-12-31 23:59:59.999999'`, `TIMESTAMPTZ '2026-10-17 16:30:00+02'`, `TIMESTAMP_NS '2262-04-11 23:47:16.854775806'`, `TIMESTAMP_NS '1677-09-21 00:12:43.145224194'`, `TIMESTAMP '2262-04-11 23:47:16.854775'`, `NULL::TIMESTAMP_NS`} {
		b, e := Execute(context.Background(), p, "SELECT "+expr, o)
		if e != nil {
			t.Errorf("finite boundary %s: %v", expr, e)
			continue
		}
		var r result
		if e = json.Unmarshal(b, &r); e != nil || len(r.Rows) != 1 {
			t.Fatalf("invalid result: %s %v", b, e)
		}
		var cells []*string
		if e = json.Unmarshal(r.Rows[0], &cells); e != nil {
			t.Fatal(e)
		}
		if cells[0] != nil {
			if _, e = time.Parse(time.RFC3339Nano, *cells[0]); e != nil {
				t.Fatalf("not RFC 3339: %s", r.Rows[0])
			}
		}
	}
}

func TestJSONTemporalExplicitCastsPreserveSpecialValues(t *testing.T) {
	p := fixture(t, "temporal.duckdb")
	o := DefaultOptions()
	o.Format = "json"
	b, e := Execute(context.Background(), p, `SELECT [CAST('infinity'::TIMESTAMP_NS AS VARCHAR)], CAST(TIMESTAMP '-infinity' AS VARCHAR), CAST(DATE '10000-01-01' AS VARCHAR)`, o)
	if e != nil || !bytes.Contains(b, []byte(`[["infinity"]`)) || !bytes.Contains(b, []byte(`"-infinity","10000-01-01"`)) {
		t.Fatalf("explicit casts: %s %v", b, e)
	}
	for _, v := range []time.Time{time.Date(-1, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)} {
		if _, e := jsonValue(v); e == nil {
			t.Fatalf("invalid RFC 3339 time accepted: %v", v)
		}
	}
}
