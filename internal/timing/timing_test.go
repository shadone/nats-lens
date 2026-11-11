package timing

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func familyLines(t *testing.T, name string) []string {
	t.Helper()
	for _, f := range Families() {
		if f.Name == name {
			return f.Lines
		}
	}
	t.Fatalf("family %s not registered", name)
	return nil
}

func TestHistogramWritesCumulativeBuckets(t *testing.T) {
	v := NewHistogramVec("test_cumulative_seconds", "h", []string{"job"}, []float64{0.1, 1})
	h := v.With("a")
	for _, x := range []float64{0.05, 0.1, 0.5, 3} {
		h.Observe(x)
	}
	got := strings.Join(familyLines(t, "test_cumulative_seconds"), "")
	want := `test_cumulative_seconds_bucket{job="a",le="0.1"} 2
test_cumulative_seconds_bucket{job="a",le="1"} 3
test_cumulative_seconds_bucket{job="a",le="+Inf"} 4
test_cumulative_seconds_sum{job="a"} 3.65
test_cumulative_seconds_count{job="a"} 4
`
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if v.Count("a") != 4 || v.Count("b") != 0 {
		t.Fatalf("count a %d, b %d", v.Count("a"), v.Count("b"))
	}
}

func TestSeriesBeyondTheBoundGoToOther(t *testing.T) {
	v := NewHistogramVec("test_bound_seconds", "h", []string{"procedure"}, []float64{1})
	for i := range MaxSeries + 10 {
		v.With(fmt.Sprint("p", i)).Observe(0.5)
	}
	if n := len(familyLines(t, "test_bound_seconds")) / 4; n != MaxSeries+1 {
		t.Fatalf("%d series, want %d", n, MaxSeries+1)
	}
	if got := v.Count(Overflow); got != 10 {
		t.Fatalf("other counted %d, want 10", got)
	}
	g := NewGaugeVec("test_bound_gauge", "g", []string{"procedure"})
	for i := range MaxSeries + 3 {
		g.With(fmt.Sprint("p", i)).Add(1)
	}
	if got := g.With(Overflow).Load(); got != 3 {
		t.Fatalf("gauge other %d, want 3", got)
	}
}

func TestQueryCountsTheCall(t *testing.T) {
	c := &Call{}
	ctx := WithCall(WithSource(context.Background(), SourceAPI), c)
	before := QueryDuration.Count("test-system", SourceAPI)
	Query(ctx, "test-system", time.Now().Add(-time.Millisecond))
	Query(ctx, "test-system", time.Now())
	if c.Queries() != 2 || c.QuerySeconds() <= 0 {
		t.Fatalf("call counted %d queries, %g s", c.Queries(), c.QuerySeconds())
	}
	if got := QueryDuration.Count("test-system", SourceAPI) - before; got != 2 {
		t.Fatalf("query histogram counted %d, want 2", got)
	}
	if Source(context.Background()) != SourceOther {
		t.Fatal("a context without a source is not other")
	}
}

func TestLabelValuesAreEscaped(t *testing.T) {
	got := line("m", 1, []string{"a"}, []string{"x\"y\\z\n"})
	if want := `m{a="x\"y\\z\n"} 1` + "\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func BenchmarkObserve(b *testing.B) {
	b.ReportAllocs()
	v := NewHistogramVec("bench_seconds", "h", []string{"system", "source"}, LatencyBuckets)
	for b.Loop() {
		v.With("default", SourceAPI).Observe(0.003)
	}
}

func BenchmarkQueryWithCall(b *testing.B) {
	b.ReportAllocs()
	ctx := WithCall(WithSource(context.Background(), SourceAPI), &Call{})
	for b.Loop() {
		Query(ctx, "default", time.Now())
	}
}
