package storeprov_test

import (
	"os"
	"strconv"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// GetSubjects over many account reports, for measuring: set
// SUBJECTS_SCALE_DAYS, SUBJECTS_SCALE_SERVERS and SUBJECTS_SCALE_ACCOUNTS
// (4, 50 and 200 make 115 million rows); skipped otherwise.
func TestSubjectsAPIRateScale(t *testing.T) {
	if testing.Short() {
		t.Skip("scale integration: run without -short")
	}
	days, _ := strconv.Atoi(os.Getenv("SUBJECTS_SCALE_DAYS"))
	if days == 0 {
		t.Skip()
	}
	servers, _ := strconv.Atoi(os.Getenv("SUBJECTS_SCALE_SERVERS"))
	accounts, _ := strconv.Atoi(os.Getenv("SUBJECTS_SCALE_ACCOUNTS"))
	last := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	st := emptyStore(t)
	db := st.DB()
	epochs := days * 24 * 120
	first := last.Add(-time.Duration(epochs-1) * 30 * time.Second)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO epochs SELECT i, ?::TIMESTAMP + to_seconds(30 * (i - 1)), ?::TIMESTAMP + to_seconds(30 * (i - 1)) + INTERVAL 1 SECOND,
  0, 9, 9, [], [], [], 0, false, [], NULL FROM range(1, ? + 1) t(i)`, first.Format(time.DateTime), first.Format(time.DateTime), epochs)
	ins := time.Now()
	exec(`INSERT INTO account_stats (epoch, server_id, account, js_streams, js_api_total, js_api_errors)
SELECT e, 'S' || s, 'A' || a, 1, 1000 + 90 * e, e FROM range(1, ? + 1) x(e), range(?) y(s), range(?) z(a)`, epochs, servers, accounts)
	t.Logf("insert %s", time.Since(ins))
	p := &storeprov.Provider{St: st, Interval: 30 * time.Second, Live: true, Clock: func() time.Time { return last.Add(10 * time.Second) },
		Retention: 14 * day}
	_, c := apitest.Serve(t, p)
	for i := 0; i < 3; i++ {
		start := time.Now()
		res := must(c.GetSubjects(ctx, connect.NewRequest(&v1.GetSubjectsRequest{})))
		t.Logf("rows %d: GetSubjects %s, A0 %v/s", epochs*servers*accounts, time.Since(start), subjRow(res, "A0", api.JetStreamAPIPattern).GetApiPerSecond())
	}
}
