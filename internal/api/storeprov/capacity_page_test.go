package storeprov_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	"github.com/shadone/nats-lens/internal/checks"
	"github.com/shadone/nats-lens/internal/config"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/inventory"
	"github.com/shadone/nats-lens/internal/store"
)

// The capacity store holds 12 hours of epochs, 10 minutes apart, ending
// at the epoch capEpochs.
const (
	capEpochs = 73
	capStep   = 10 * time.Minute
)

// capInventory declares ledger unbounded on purpose and lists the KV
// bucket flags.
const capInventory = `accounts:
  - name: APP
    streams:
      - name: ledger
        max_age: 0
        max_bytes: -1
    kv:
      - name: flags
`

// capStream is one stream of the capacity store: its configuration on
// nats-0, which leads it, and its numbers in epoch e.
type capStream struct {
	name, retention, storage, discard, mirror string
	sources                                   string // a DuckDB list literal
	replicas                                  int32
	maxMsgs, maxBytes, maxAgeNs, perSubject   int64
	allowMsgTTL                               bool
	// numbers returns messages, bytes and the age of the oldest message
	// (0: empty) in epoch e.
	numbers func(e int64) (int64, int64, time.Duration)
	keys    int64
}

func steady(msgs, bytes int64, held time.Duration) func(int64) (int64, int64, time.Duration) {
	return func(int64) (int64, int64, time.Duration) { return msgs, bytes, held }
}

// capacityStore writes a system in which every state of the Capacity page
// can be found. Servers: nats-0 grows 10 MiB an epoch in a 50 GiB store
// and server-014 fires on it; nats-1 reports only for the last 5 hours,
// without a limit; nats-2 missed the newest epoch; nats-3 runs without
// JetStream. Streams, each led by nats-0:
//   - orders: limits, 7 days, the oldest message 4 days old; grows.
//   - jobs: work queue, empty.
//   - orders-copy: mirror of orders at 995 of 1000 messages, discard new.
//   - feed: sources two streams, 10 GB, grows 1 MB an epoch.
//   - cache: memory, an hour, at its limit.
//   - archive, wishlist, payments, scratch: no limit; stream-005 fires on
//     the first three, wishlist is accepted, payments muted; scratch has
//     no finding.
//   - ledger: no limit, declared in the inventory.
//   - KV_flags: listed in the inventory, per-message TTLs, history 1.
//   - big: 2 GB with discard new; stream-006 fires.
//   - OBJ_images: 48 hours and 8 GiB, at its age limit.
//   - KV_shrink: 100 MB, shrinking.
//   - spread: no leader in the newest epoch, two reports that differ.
func capacityStore(t *testing.T, last time.Time) (*store.Store, *checks.Set) {
	t.Helper()
	st := emptyStore(t)
	r := rows{t, st}
	at := func(e int64) time.Time { return last.Add(-time.Duration(capEpochs-e) * capStep) }
	names := []string{"nats-0", "nats-1", "nats-2", "nats-3"}
	for i, n := range names {
		r.add("servers", map[string]any{"server_id": fmt.Sprintf("S%d", i), "server_name": n, "cluster": "c1", "jetstream": i != 3,
			"first_epoch": 1, "last_epoch": capEpochs, "first_seen_at": at(1), "last_seen_at": at(capEpochs)})
	}
	streams := []capStream{
		{name: "orders", replicas: 3, maxAgeNs: int64(7 * day), numbers: func(e int64) (int64, int64, time.Duration) {
			return 1000 * e, 1_000_000_000 + 2_000_000*e, 4 * day
		}},
		{name: "jobs", retention: "workqueue", replicas: 3, maxAgeNs: int64(day), maxMsgs: 1_000_000, numbers: steady(0, 0, 0)},
		{name: "orders-copy", mirror: "orders", replicas: 1, maxMsgs: 1000, discard: "new", numbers: steady(995, 5000, day)},
		{name: "feed", sources: "['orders', 'jobs']", replicas: 3, maxBytes: 10_000_000_000, numbers: func(e int64) (int64, int64, time.Duration) {
			return 10 * e, 1_000_000 * e, day
		}},
		{name: "cache", storage: "memory", replicas: 1, maxAgeNs: int64(time.Hour), numbers: steady(30, 3000, time.Hour)},
		{name: "archive", replicas: 3, numbers: steady(100, 100_000_000, 300*day)},
		{name: "wishlist", replicas: 3, numbers: steady(100, 1000, 30*day)},
		{name: "payments", replicas: 3, numbers: steady(100, 1000, 30*day)},
		{name: "scratch", replicas: 1, numbers: steady(5, 500, day)},
		{name: "ledger", replicas: 3, numbers: steady(5, 500, 200*day)},
		{name: "KV_flags", replicas: 3, discard: "new", perSubject: 1, allowMsgTTL: true, keys: 12, numbers: steady(12, 1200, 9*day)},
		{name: "big", replicas: 3, maxBytes: 2_000_000_000, discard: "new", numbers: func(e int64) (int64, int64, time.Duration) {
			return e, 1_000_000_000 + 10_000_000*e, 20 * day
		}},
		{name: "OBJ_images", replicas: 3, discard: "new", maxAgeNs: int64(48 * time.Hour), maxBytes: 8 << 30, numbers: steady(40, 40_000_000, 48*time.Hour)},
		{name: "KV_shrink", replicas: 3, discard: "new", perSubject: 1, maxBytes: 100_000_000, keys: 4, numbers: func(e int64) (int64, int64, time.Duration) {
			return 1000 - e, 50_000_000 - 100_000*e, day
		}},
	}
	for e := int64(1); e <= capEpochs; e++ {
		missing := "[]"
		if e == capEpochs {
			missing = "['nats-2']"
		}
		if _, err := st.DB().Exec(fmt.Sprintf(`INSERT INTO epochs VALUES (?, ?, ?, 0, 4, 3, ['nats-0', 'nats-1', 'nats-2', 'nats-3'], [], %s, 0, ?, [], NULL)`, missing),
			e, at(e), at(e).Add(time.Second), e == capEpochs); err != nil {
			t.Fatal(err)
		}
		r.add("server_stats", map[string]any{"epoch": e, "server_id": "S0", "js_enabled": true, "js_storage": int64(1<<30 + 10<<20*e), "js_max_storage": int64(50 << 30)})
		if e > capEpochs-30 {
			r.add("server_stats", map[string]any{"epoch": e, "server_id": "S1", "js_enabled": true, "js_storage": int64(2 << 30), "js_max_storage": 0})
		}
		if e < capEpochs {
			r.add("server_stats", map[string]any{"epoch": e, "server_id": "S2", "js_enabled": true, "js_storage": int64(3 << 30), "js_max_storage": int64(50 << 30)})
		}
		r.add("server_stats", map[string]any{"epoch": e, "server_id": "S3", "js_enabled": false})
		for _, s := range streams {
			msgs, bytes, held := s.numbers(e)
			row := map[string]any{"epoch": e, "server_id": "S0", "account": "APP", "stream": s.name, "is_leader": true, "leader": "nats-0",
				"messages": msgs, "bytes": bytes, "num_subjects": s.keys}
			if held > 0 {
				row["first_ts"] = at(e).Add(-held)
			}
			r.add("stream_stats", row)
		}
	}
	// spread has no leader in the newest epoch and its two reports differ:
	// the largest is read, as ListStreams reads it.
	for i, b := range []int64{20_000, 36_000} {
		r.add("stream_stats", map[string]any{"epoch": int64(capEpochs), "server_id": fmt.Sprintf("S%d", i), "account": "APP", "stream": "spread",
			"is_leader": false, "leader": "", "messages": b / 100, "bytes": b, "first_ts": at(capEpochs).Add(-time.Duration(i+1) * time.Hour)})
	}
	r.add("stream_config", map[string]any{"server_id": "S0", "account": "APP", "stream": "spread", "first_epoch": 1, "last_epoch": capEpochs,
		"config_hash": "h", "retention": "limits", "storage": "file", "replicas": 2, "max_msgs": -1, "max_bytes": -1, "max_age_ns": int64(7 * day),
		"max_msgs_per_subject": -1, "discard": "old"})
	// A follower of orders reports other numbers; the leader's are read.
	r.add("stream_stats", map[string]any{"epoch": int64(capEpochs), "server_id": "S1", "account": "APP", "stream": "orders",
		"is_leader": false, "leader": "nats-0", "messages": 1, "bytes": 1})
	none := func(v int64) int64 {
		if v == 0 {
			return -1
		}
		return v
	}
	for _, s := range streams {
		r.add("streams", map[string]any{"account": "APP", "stream": s.name, "first_epoch": 1, "last_epoch": capEpochs})
		ret, storage, discard := s.retention, s.storage, s.discard
		if ret == "" {
			ret = "limits"
		}
		if storage == "" {
			storage = "file"
		}
		if discard == "" {
			discard = "old"
		}
		r.add("stream_config", map[string]any{"server_id": "S0", "account": "APP", "stream": s.name, "first_epoch": 1, "last_epoch": capEpochs,
			"config_hash": "h", "retention": ret, "storage": storage, "replicas": s.replicas, "max_msgs": none(s.maxMsgs), "max_bytes": none(s.maxBytes),
			"max_age_ns": s.maxAgeNs, "max_msgs_per_subject": none(s.perSubject), "discard": discard, "mirror": s.mirror, "allow_msg_ttl": s.allowMsgTTL})
		if s.sources != "" {
			if _, err := st.DB().Exec(`UPDATE stream_config SET sources = `+s.sources+` WHERE stream = ?`, s.name); err != nil {
				t.Fatal(err)
			}
		}
	}
	p := filepath.Join(t.TempDir(), "inventory.yaml")
	if err := os.WriteFile(p, []byte(capInventory), 0o600); err != nil {
		t.Fatal(err)
	}
	inv, err := inventory.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceInventory(ctx, inv, p, at(1)); err != nil {
		t.Fatal(err)
	}
	set, err := checks.Load(config.Checks{Timeout: config.Duration(5 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	unbounded := func(name string) store.FindingRow {
		return store.FindingRow{EntityKey: "APP/" + name, Severity: "info", Detail: name + " has no limits."}
	}
	days := func(v float64) *float64 { return &v }
	for e := int64(capEpochs - 3); e <= capEpochs; e++ {
		found := map[string][]store.FindingRow{
			"stream-005": {unbounded("archive"), unbounded("wishlist"), unbounded("payments")},
			"stream-006": {{EntityKey: "APP/big", Severity: "warning", Detail: "big reaches max_bytes soon.", Value: days(5.5)}},
			"server-014": {{EntityKey: "nats-0", Severity: "warning", Detail: "nats-0 full soon.", Value: days(12.25)}},
		}
		var runs []store.CheckRun
		for _, c := range set.Checks() {
			runs = append(runs, store.CheckRun{Code: c.Code, EntityType: c.Entity, Status: store.RunOK, Rows: found[c.Code]})
		}
		if _, err := st.ApplyCheckRuns(ctx, e, runs); err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range []store.Mute{
		{CheckCode: "stream-005", EntityType: "stream", EntityKey: "APP/wishlist", Reason: "Wishes are kept for the account's life.",
			MutedBy: "ops@example.com", CreatedAt: at(capEpochs - 1), ExpiresAt: last.Add(180 * day), Accepted: true},
		{CheckCode: "stream-005", EntityType: "stream", EntityKey: "APP/payments", Reason: "Limits in review.",
			MutedBy: "ops", CreatedAt: at(capEpochs - 1), ExpiresAt: last.Add(day)},
	} {
		if _, err := st.CreateMute(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	return st, set
}

// capacityProvider serves the capacity store with its clock 30 seconds
// after the newest epoch.
func capacityProvider(t *testing.T, last time.Time) *storeprov.Provider {
	t.Helper()
	st, set := capacityStore(t, last)
	clock := last.Add(30 * time.Second)
	return &storeprov.Provider{St: st, Interval: capStep, Live: true, Checks: set, Clock: func() time.Time { return clock }, Retention: 14 * day}
}

// The store reads every part of the page from the epoch and the range.
func TestCapacityFromStore(t *testing.T) {
	last := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	_, c := apitest.Serve(t, capacityProvider(t, last))
	day24 := &v1.TimeRange{Start: timestamppb.New(last.Add(-24 * time.Hour)), End: timestamppb.New(last.Add(time.Minute))}
	res := must(c.GetCapacity(ctx, connect.NewRequest(&v1.GetCapacityRequest{Range: day24})))
	if res.GetEpoch() != capEpochs || !res.GetEpochAt().AsTime().Equal(last) || !res.GetGrowthFrom().AsTime().Equal(last.Add(-12*time.Hour)) ||
		res.GetServersWithoutJetstream() != 1 || res.GetStreamsTotal() != 15 {
		t.Fatalf("response = epoch %d at %v from %v, %d without JetStream, %d streams", res.GetEpoch(), res.GetEpochAt().AsTime(),
			res.GetGrowthFrom().AsTime(), res.GetServersWithoutJetstream(), res.GetStreamsTotal())
	}
	srv := map[string]*v1.ServerStore{}
	for _, s := range res.GetServers() {
		srv[s.GetServerName()] = s
	}
	// nats-0 grows 10 MiB every 10 minutes: 1440 MiB a day.
	if s := srv["nats-0"]; !s.GetReported() || s.GetUsed() != 1<<30+10<<20*capEpochs || s.GetLimit() != 50<<30 ||
		s.GetPerDay() != 1440<<20 || s.DaysToFull == nil || s.GetFindingCode() != "server-014" || s.GetFindingDays() != 12.25 {
		t.Errorf("nats-0 = %v", s)
	}
	if s := srv["nats-1"]; !s.GetReported() || s.Limit != nil || s.PerDay != nil || s.DaysToFull != nil {
		t.Errorf("nats-1 = %v", s)
	}
	if s := srv["nats-2"]; s.GetReported() || s.Used != nil || s.PerDay != nil {
		t.Errorf("nats-2 = %v", s)
	}
	if srv["nats-3"] != nil || len(srv) != 3 {
		t.Errorf("servers = %v", res.GetServers())
	}
	by := map[string]*v1.CapacityStream{}
	var order []string
	for _, s := range res.GetStreams() {
		by[s.GetStream()] = s
		order = append(order, s.GetStream())
	}
	if order[0] != "big" || order[1] != "archive" {
		t.Errorf("order %v: fills soon, then awaiting a decision first", order)
	}
	type want struct {
		status v1.CapacityStatus
		limit  v1.NextLimitKind
		state  v1.NextLimitState
	}
	for name, w := range map[string]want{
		"orders":      {v1.CapacityStatus_CAPACITY_STATUS_BOUNDED, v1.NextLimitKind_NEXT_LIMIT_KIND_AGE, v1.NextLimitState_NEXT_LIMIT_STATE_REACHES},
		"jobs":        {v1.CapacityStatus_CAPACITY_STATUS_BOUNDED, v1.NextLimitKind_NEXT_LIMIT_KIND_EMPTY, 0},
		"orders-copy": {v1.CapacityStatus_CAPACITY_STATUS_BOUNDED, v1.NextLimitKind_NEXT_LIMIT_KIND_MESSAGES, v1.NextLimitState_NEXT_LIMIT_STATE_REACHED_REFUSING},
		"feed":        {v1.CapacityStatus_CAPACITY_STATUS_BOUNDED, v1.NextLimitKind_NEXT_LIMIT_KIND_BYTES, v1.NextLimitState_NEXT_LIMIT_STATE_REACHES},
		"cache":       {v1.CapacityStatus_CAPACITY_STATUS_BOUNDED, v1.NextLimitKind_NEXT_LIMIT_KIND_AGE, v1.NextLimitState_NEXT_LIMIT_STATE_STEADY},
		"archive":     {v1.CapacityStatus_CAPACITY_STATUS_AWAITING_DECISION, v1.NextLimitKind_NEXT_LIMIT_KIND_NONE, 0},
		"wishlist":    {v1.CapacityStatus_CAPACITY_STATUS_ACCEPTED, v1.NextLimitKind_NEXT_LIMIT_KIND_NONE, 0},
		"payments":    {v1.CapacityStatus_CAPACITY_STATUS_MUTED, v1.NextLimitKind_NEXT_LIMIT_KIND_NONE, 0},
		"scratch":     {v1.CapacityStatus_CAPACITY_STATUS_UNBOUNDED, v1.NextLimitKind_NEXT_LIMIT_KIND_NONE, 0},
		"ledger":      {v1.CapacityStatus_CAPACITY_STATUS_DECLARED, v1.NextLimitKind_NEXT_LIMIT_KIND_NONE, 0},
		"KV_flags":    {v1.CapacityStatus_CAPACITY_STATUS_DECLARED, v1.NextLimitKind_NEXT_LIMIT_KIND_PER_SUBJECT, v1.NextLimitState_NEXT_LIMIT_STATE_NOT_FORECAST},
		"big":         {v1.CapacityStatus_CAPACITY_STATUS_FILLS_SOON, v1.NextLimitKind_NEXT_LIMIT_KIND_BYTES, v1.NextLimitState_NEXT_LIMIT_STATE_REACHES},
		"OBJ_images":  {v1.CapacityStatus_CAPACITY_STATUS_BOUNDED, v1.NextLimitKind_NEXT_LIMIT_KIND_AGE, v1.NextLimitState_NEXT_LIMIT_STATE_STEADY},
		"KV_shrink":   {v1.CapacityStatus_CAPACITY_STATUS_BOUNDED, v1.NextLimitKind_NEXT_LIMIT_KIND_BYTES, v1.NextLimitState_NEXT_LIMIT_STATE_NOT_REACHED},
	} {
		s := by[name]
		if s.GetStatus() != w.status || s.GetNextLimit().GetLimit() != w.limit || s.GetNextLimit().GetState() != w.state {
			t.Errorf("%s: status %v, next limit %v; want %v", name, s.GetStatus(), s.GetNextLimit(), w)
		}
	}
	if s := by["orders"]; s.GetMessages() != 1000*capEpochs || s.GetReplicas() != 3 || s.GetBytesPerDay() != 288_000_000 ||
		s.GetMsgsPerDay() != 144_000 || s.GetNextLimit().GetDays() != 3 || !s.GetFirstTs().AsTime().Equal(last.Add(-4*day)) ||
		s.GetMaxAge().AsDuration() != 7*day || s.MaxMsgs != nil || s.MaxBytes != nil || s.GetRetention() != "limits" || s.GetKind() != v1.CapacityStreamKind_CAPACITY_STREAM_KIND_STREAM {
		t.Errorf("orders = %v", s)
	}
	if s := by["spread"]; s.GetBytes() != 36_000 || s.GetMessages() != 360 || !s.GetFirstTs().AsTime().Equal(last.Add(-2*time.Hour)) {
		t.Errorf("spread = %v", s)
	}
	if s := by["jobs"]; s.FirstTs != nil || s.GetRetention() != "workqueue" {
		t.Errorf("jobs = %v", s)
	}
	if s := by["orders-copy"]; s.GetMirror() != "orders" || !s.GetNextLimit().GetDiscardNew() {
		t.Errorf("orders-copy = %v", s)
	}
	if s := by["feed"]; s.GetSources() != 2 || s.GetBytesPerDay() != 144_000_000 {
		t.Errorf("feed = %v", s)
	}
	if by["cache"].GetStorage() != "memory" {
		t.Errorf("cache = %v", by["cache"])
	}
	if s := by["KV_flags"]; s.GetKind() != v1.CapacityStreamKind_CAPACITY_STREAM_KIND_KEY_VALUE || s.GetKeys() != 12 ||
		s.GetDeclaredReason() != api.DeclaredWriterTTLs || !s.GetAllowMsgTtl() || s.GetMaxMsgsPerSubject() != 1 {
		t.Errorf("KV_flags = %v", s)
	}
	if by["ledger"].GetDeclaredReason() != api.DeclaredInventory {
		t.Errorf("ledger = %v", by["ledger"])
	}
	if s := by["wishlist"]; !s.GetMute().GetAccepted() || s.GetMute().GetMutedBy() != "ops@example.com" || s.GetFindingId() != "stream-005:APP/wishlist" {
		t.Errorf("wishlist = %v", s)
	}
	if s := by["big"]; s.GetFindingDays() != 5.5 || s.GetFindingId() != "stream-006:APP/big" || !s.GetNextLimit().GetDiscardNew() {
		t.Errorf("big = %v", s)
	}
	if s := by["OBJ_images"]; s.GetKind() != v1.CapacityStreamKind_CAPACITY_STREAM_KIND_OBJECT_STORE || s.Keys != nil {
		t.Errorf("OBJ_images = %v", s)
	}
	// A range without an epoch.
	old := &v1.TimeRange{Start: timestamppb.New(last.Add(-48 * time.Hour)), End: timestamppb.New(last.Add(-36 * time.Hour))}
	if res := must(c.GetCapacity(ctx, connect.NewRequest(&v1.GetCapacityRequest{Range: old}))); res.GetEpoch() != 0 || len(res.GetStreams()) != 0 {
		t.Errorf("no epoch: %v", res)
	}
}
