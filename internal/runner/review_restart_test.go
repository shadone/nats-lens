package runner

import (
	"context"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/store"
)

// nats-0 restarts with a new server_id two days before the review; its
// js_storage continues at +10 MiB/day. Matched by name, the review keeps
// the acceptance.
func TestReviewFollowsAServerAcrossARestart(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	const epochs = 9 * 2880
	for _, q := range []string{
		`INSERT INTO servers (server_id, server_name, first_epoch, last_epoch, first_seen_at, last_seen_at) VALUES
  ('NOLD', 'nats-0', 1, 20000, TIMESTAMP '2026-10-20', TIMESTAMP '2026-10-20'),
  ('NNEW', 'nats-0', 20001, 30000, TIMESTAMP '2026-10-20', TIMESTAMP '2026-10-20')`,
		`INSERT INTO epochs (epoch, started_at, finished_at, duration_ms, expected_servers, observed_servers,
  expected_names, observed_names, missing_names, endpoint_failures, partial, errors)
SELECT i + 1, TIMESTAMP '2026-10-20 00:00:00' + to_seconds(i * 30), TIMESTAMP '2026-10-20 00:00:00' + to_seconds(i * 30),
  0, 1, 1, ['nats-0'], ['nats-0'], [], 0, false, [] FROM range(25920) r(i)`,
		`INSERT INTO server_stats (epoch, server_id, js_storage)
SELECT i + 1, CASE WHEN i + 1 <= 20000 THEN 'NOLD' ELSE 'NNEW' END, (1 << 30) + (i * 10 * 1048576) // 2880
FROM range(25920) r(i)`,
	} {
		if _, err := st.DB().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	accept(t, st, "server", "nats-0", f(10*mib))
	b := accept(t, st, "server", "nats-0", nil)
	rev, err := reviewAcceptances(ctx, st, epochs, 30*time.Second, serverToo)
	if err != nil {
		t.Fatal(err)
	}
	if len(rev.Ended) != 0 {
		t.Errorf("ended across a restart: %s", rev.Ended[0].EndReason)
	}
	if len(rev.Baselined) != 1 || rev.Baselined[0].ID != b.ID || *rev.Baselined[0].GrowthPerDay < 9.9*mib || *rev.Baselined[0].GrowthPerDay > 10.1*mib {
		t.Errorf("baselined %+v, want b at 10 MiB/day", rev.Baselined)
	}
}
