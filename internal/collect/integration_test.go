package collect_test

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/shadone/nats-lens/internal/collect"
	"github.com/shadone/nats-lens/internal/store"
	"github.com/shadone/nats-lens/internal/testcluster"
)

// createAssets makes shop-shaped R3 and R1 assets and returns the name of
// the server hosting the R1 consumer.
func createAssets(t *testing.T, ctx context.Context, js jetstream.JetStream) string {
	t.Helper()
	jobs, err := js.CreateStream(ctx, jetstream.StreamConfig{
		Name: "order-jobs", Subjects: []string{"orders.>"}, Replicas: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := jobs.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable: "resize-worker", AckPolicy: jetstream.AckExplicitPolicy, MaxDeliver: 3,
		FilterSubject: "orders.*.*.resize.created",
	}); err != nil {
		t.Fatal(err)
	}
	r1, err := jobs.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable: "job-cache-projector-r1", AckPolicy: jetstream.AckExplicitPolicy, Replicas: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := js.CreateStream(ctx, jetstream.StreamConfig{
		Name: "worker-presence", Subjects: []string{"worker.presence.>"}, Replicas: 1,
	}); err != nil {
		t.Fatal(err)
	}
	for i := range 20 {
		if _, err := js.Publish(ctx, "orders.p1.j"+string(rune('a'+i))+".resize.created", []byte("job")); err != nil {
			t.Fatal(err)
		}
	}
	info, err := r1.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.Cluster == nil || info.Cluster.Leader == "" {
		t.Fatal("R1 consumer has no host")
	}
	return info.Cluster.Leader
}

func TestCollectFromCluster(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a 3-node cluster")
	}
	ctx := context.Background()
	c := testcluster.Start(t, 3)
	js, err := jetstream.New(c.Connect(0, testcluster.AppUser, testcluster.AppPass))
	if err != nil {
		t.Fatal(err)
	}
	r1Host := createAssets(t, ctx, js)

	st, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	col := collect.New(c.Connect(0, testcluster.SysUser, testcluster.SysPass), collect.Options{
		Window: time.Second, ConnzLimit: 100, Tracker: collect.NewTracker(time.Hour),
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	// Epoch 1: all three servers.
	snap := col.Collect(ctx)
	if got := snap.Observed(); !slices.Equal(got, c.Names) {
		t.Fatalf("observed = %v, want %v (errors %v)", got, c.Names, snap.Errors)
	}
	for _, v := range snap.Servers {
		for e, s := range v.Status {
			if s.State != collect.StateOK {
				t.Errorf("%s %s: %+v", v.Info.Name, e, s)
			}
		}
	}
	if snap.Partial() {
		t.Fatalf("first epoch partial: missing=%v errors=%v", snap.Missing(), snap.Errors)
	}
	if _, err := st.WriteSnapshot(ctx, snap); err != nil {
		t.Fatal(err)
	}
	db := st.DB()
	count := func(q string, args ...any) int {
		t.Helper()
		var n int
		if err := db.QueryRow(q, args...).Scan(&n); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return n
	}
	for _, name := range c.Names {
		for _, tbl := range []string{"server_stats", "health"} {
			if n := count("SELECT count(*) FROM "+tbl+" JOIN servers USING (server_id) WHERE server_name = ?", name); n != 1 {
				t.Errorf("%s rows for %s = %d", tbl, name, n)
			}
		}
		if n := count(`SELECT count(*) FROM stream_stats JOIN servers USING (server_id)
WHERE server_name = ? AND stream = 'order-jobs'`, name); n != 1 {
			t.Errorf("order-jobs on %s = %d", name, n)
		}
		if n := count(`SELECT count(*) FROM consumer_stats JOIN servers USING (server_id)
WHERE server_name = ? AND consumer = 'resize-worker'`, name); n != 1 {
			t.Errorf("resize-worker on %s = %d", name, n)
		}
		if n := count(`SELECT count(*) FROM raft_stats JOIN servers USING (server_id)
WHERE server_name = ? AND group_id = '_meta_'`, name); n != 1 {
			t.Errorf("meta raft group on %s = %d", name, n)
		}
		// Stream raft groups live under the asset's account, not the
		// system account, and need their own RAFTZ request.
		if n := count(`SELECT count(*) FROM raft_stats r JOIN servers USING (server_id)
JOIN raft_groups g USING (group_id)
WHERE server_name = ? AND g.kind = 'stream' AND g.stream = 'order-jobs' AND r.leader_name IS NOT NULL`, name); n != 1 {
			t.Errorf("order-jobs raft group on %s = %d", name, n)
		}
	}
	if n := count(`SELECT count(*) FROM consumer_stats WHERE consumer = 'job-cache-projector-r1'`); n != 1 {
		t.Fatalf("R1 consumer rows = %d, want exactly 1", n)
	}
	var host string
	if err := db.QueryRow(`SELECT server_name FROM consumer_stats JOIN servers USING (server_id)
WHERE consumer = 'job-cache-projector-r1'`).Scan(&host); err != nil {
		t.Fatal(err)
	}
	if host != r1Host {
		t.Errorf("R1 consumer reported by %s, hosted on %s", host, r1Host)
	}

	// The resolved raft leader name is the stream's real leader.
	jobs, err := js.Stream(ctx, "order-jobs")
	if err != nil {
		t.Fatal(err)
	}
	if n := count(`SELECT count(DISTINCT r.leader_name) FROM raft_stats r JOIN raft_groups g USING (group_id)
WHERE g.kind = 'stream' AND g.stream = 'order-jobs' AND r.leader_name = ?`, jobs.CachedInfo().Cluster.Leader); n != 1 {
		t.Errorf("raft leader_name does not match the stream leader %s", jobs.CachedInfo().Cluster.Leader)
	}

	// Only the consumer leader reports the backlog: 20 resize jobs were
	// published and none consumed.
	embed, err := jobs.Consumer(ctx, "resize-worker")
	if err != nil {
		t.Fatal(err)
	}
	embedLeader := embed.CachedInfo().Cluster.Leader
	if n := count(`SELECT count(*) FROM consumer_stats JOIN servers USING (server_id)
WHERE consumer = 'resize-worker' AND server_name = ? AND is_leader AND num_pending = 20`, embedLeader); n != 1 {
		t.Errorf("leader %s row lacks num_pending = 20", embedLeader)
	}
	if n := count(`SELECT count(*) FROM consumer_stats
WHERE consumer = 'resize-worker' AND NOT is_leader AND num_pending IS NULL AND num_waiting IS NULL`); n != 2 {
		t.Errorf("follower rows with NULL counters = %d, want 2", n)
	}

	// With every server known, discovery and details stop as soon as all
	// three have answered instead of waiting the 1s window.
	snap = col.Collect(ctx)
	if d := snap.FinishedAt.Sub(snap.StartedAt); d > 500*time.Millisecond || snap.Partial() {
		t.Errorf("epoch with every server known took %s (partial %v)", d, snap.Partial())
	}
	if _, err := st.WriteSnapshot(ctx, snap); err != nil {
		t.Fatal(err)
	}
	if n := count(`SELECT count(*) FROM stream_stats WHERE stream = 'worker-presence' AND epoch = 1`); n != 1 {
		t.Errorf("R1 stream rows = %d", n)
	}
	if n := count(`SELECT count(*) FROM connections WHERE account = 'APP'`); n == 0 {
		t.Error("app connection not recorded")
	}

	// Epoch 3: one server down. It stays expected and is reported missing.
	c.Stop(2)
	snap = col.Collect(ctx)
	if !snap.Partial() {
		t.Fatal("epoch with a stopped server must be partial")
	}
	if got := snap.Missing(); !slices.Equal(got, []string{"nats-2"}) {
		t.Fatalf("missing = %v", got)
	}
	epoch, err := st.WriteSnapshot(ctx, snap)
	if err != nil {
		t.Fatal(err)
	}
	var expected, observed int
	var partial bool
	if err := db.QueryRow(`SELECT expected_servers, observed_servers, partial FROM epochs WHERE epoch = ?`, epoch).
		Scan(&expected, &observed, &partial); err != nil {
		t.Fatal(err)
	}
	if expected != 3 || observed != 2 || !partial {
		t.Fatalf("epoch 3: expected=%d observed=%d partial=%v", expected, observed, partial)
	}
	if n := count(`SELECT count(*) FROM server_stats WHERE epoch = ?`, epoch); n != 2 {
		t.Errorf("server_stats rows in epoch 3 = %d", n)
	}

	// Epoch 4: nats-2 is back as a new process with a new server ID.
	c.Restart(2)
	snap = col.Collect(ctx)
	if got := snap.Observed(); !slices.Equal(got, c.Names) {
		t.Fatalf("after restart observed = %v", got)
	}
	if _, err := st.WriteSnapshot(ctx, snap); err != nil {
		t.Fatal(err)
	}
	if n := count(`SELECT count(DISTINCT server_id) FROM servers WHERE server_name = 'nats-2'`); n != 2 {
		t.Errorf("nats-2 server IDs = %d, want 2 after a restart", n)
	}
}

// The scoped system user of the README collects every endpoint.
func TestCollectWithScopedSysUser(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a 3-node cluster")
	}
	ctx := context.Background()
	c := testcluster.Start(t, 3)
	js, err := jetstream.New(c.Connect(0, testcluster.AppUser, testcluster.AppPass))
	if err != nil {
		t.Fatal(err)
	}
	createAssets(t, ctx, js)
	col := collect.New(c.Connect(0, testcluster.ScopedSysUser, testcluster.ScopedSysPass), collect.Options{
		Window: time.Second, ConnzLimit: 100, Tracker: collect.NewTracker(time.Hour),
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	snap := col.Collect(ctx)
	if got := snap.Observed(); !slices.Equal(got, c.Names) {
		t.Fatalf("observed = %v, want %v (errors %v)", got, c.Names, snap.Errors)
	}
	for _, v := range snap.Servers {
		for e, s := range v.Status {
			if s.State != collect.StateOK {
				t.Errorf("%s %s: %+v", v.Info.Name, e, s)
			}
		}
	}
	if snap.Partial() {
		t.Fatalf("epoch partial: missing=%v errors=%v", snap.Missing(), snap.Errors)
	}
}
