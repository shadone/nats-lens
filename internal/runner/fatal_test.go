package runner

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	duckdb "github.com/duckdb/duckdb-go/v2"
	"github.com/shadone/nats-lens/internal/store"
)

func TestRunStopsOnWrappedFatalSweep(t *testing.T) {
	st, err := store.OpenPath(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var logs bytes.Buffer
	r := &Runner{Store: st, Interval: time.Millisecond, Retention: time.Hour, Connected: func() bool { return false }, Log: slog.New(slog.NewTextHandler(&logs, nil))}
	calls := 0
	r.NotifySweep = func(context.Context, time.Time) error {
		calls++
		return fmt.Errorf("outer secret: %w", &duckdb.Error{Type: duckdb.ErrorTypeFatal, Msg: "sensitive\nChunk: " + strings.Repeat("private-row", 20000)})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err = r.Run(ctx)
	if !store.IsFatal(err) {
		t.Fatalf("Run returned %T, want typed fatal", err)
	}
	if ctx.Err() != nil {
		t.Error("Run waited for cancellation after fatal")
	}
	if calls != 1 {
		t.Errorf("sweep calls=%d", calls)
	}
	if strings.Contains(logs.String(), "private-row") || strings.Contains(logs.String(), "outer secret") || logs.Len() > 1024 {
		t.Error("runner leaked diagnostic")
	}
}
