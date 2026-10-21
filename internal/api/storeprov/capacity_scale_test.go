package storeprov_test

import (
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// GetCapacity reads 2,000 streams over a day of epochs 30 seconds apart
// (5.8 million stream rows) in a few queries; the test logs the time.
func TestCapacityManyStreams(t *testing.T) {
	if testing.Short() {
		t.Skip("writes 5.8 million rows")
	}
	const streams, epochs = 2000, 2880
	st := emptyStore(t)
	last := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	for _, q := range []string{
		`INSERT INTO servers (server_id, server_name, first_epoch, last_epoch, first_seen_at, last_seen_at)
         SELECT 'S' || i, 'nats-' || i, 1, 2880, TIMESTAMP '2026-10-16 14:00:30', TIMESTAMP '2026-10-17 14:00:00' FROM range(3) t(i)`,
		`INSERT INTO epochs SELECT e, TIMESTAMP '2026-10-17 14:00:00' - to_seconds((2880 - e) * 30), TIMESTAMP '2026-10-17 14:00:00' - to_seconds((2880 - e) * 30),
         0, 3, 3, ['nats-0', 'nats-1', 'nats-2'], [], [], 0, false, [], NULL FROM range(1, 2881) t(e)`,
		`INSERT INTO server_stats (epoch, server_id, js_enabled, js_storage, js_max_storage)
         SELECT e, 'S' || i, true, 1000000 * e, 100000000000 FROM range(1, 2881) a(e), range(3) b(i)`,
		`INSERT INTO stream_config (server_id, account, stream, first_epoch, last_epoch, config_hash, retention, storage, replicas,
           max_msgs, max_bytes, max_age_ns, max_msgs_per_subject, discard)
         SELECT 'S0', 'APP', 's' || i, 1, 2880, 'h', 'limits', 'file', 3, -1, CASE WHEN i % 2 = 0 THEN 10000000000 ELSE -1 END,
           0, -1, 'old' FROM range(2000) t(i)`,
		`INSERT INTO stream_stats (epoch, server_id, account, stream, is_leader, leader, messages, bytes, first_ts)
         SELECT e, 'S0', 'APP', 's' || i, true, 'nats-0', 10 * e + i, 1000 * e + i, TIMESTAMP '2026-10-01 00:00:00'
         FROM range(1, 2881) a(e), range(2000) b(i)`,
	} {
		if _, err := st.DB().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	clock := last.Add(30 * time.Second)
	_, c := apitest.Serve(t, &storeprov.Provider{St: st, Interval: 30 * time.Second, Live: true, Clock: func() time.Time { return clock }})
	r := &v1.TimeRange{Start: timestamppb.New(clock.Add(-24 * time.Hour)), End: timestamppb.New(clock)}
	begin := time.Now()
	res := must(c.GetCapacity(ctx, connect.NewRequest(&v1.GetCapacityRequest{Range: r})))
	took := time.Since(begin)
	if res.GetStreamsTotal() != streams || len(res.GetStreams()) != 500 || len(res.GetServers()) != 3 || res.GetStreams()[0].BytesPerDay == nil {
		t.Fatalf("%d of %d streams, %d servers", len(res.GetStreams()), res.GetStreamsTotal(), len(res.GetServers()))
	}
	t.Logf("GetCapacity over %d streams and %d epochs took %s", streams, epochs, took)
}
