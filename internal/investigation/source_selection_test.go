package investigation

import (
 "bytes"
 "context"
 "strings"
 "testing"
)

func TestSourceSelectionContract(t *testing.T) {
 for _, args := range [][]string{
  {"query", "ListMetrics", "-db", "fixture.duckdb", "-endpoint", "http://127.0.0.1:8080"},
  {"query", "ListMetrics", "-db", "fixture.duckdb", "-bundle", "fixture"},
  {"query", "ListMetrics", "-db", "fixture.duckdb", "-profiles", "missing", "-profile", "shop"},
 } {
  var out, diagnostic bytes.Buffer
  err := Run(context.Background(), args, strings.NewReader(""), &out, &diagnostic)
  if err == nil || !strings.Contains(out.String(), "choose exactly one source") { t.Errorf("%v: %v %s", args, err, out.String()) }
 }
 var out bytes.Buffer
 if err := Run(context.Background(), []string{"methods"}, strings.NewReader(""), &out, &out); err != nil || !strings.Contains(out.String(), "offline") { t.Fatalf("offline discovery missing: %v %s", err, out.String()) }
}
