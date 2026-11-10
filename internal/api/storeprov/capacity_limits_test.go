package storeprov_test

import (
	"fmt"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
)

// Account public keys of the capacity limits fixture.
const (
	keyLimited = "ALIMFAKEACCOUNTKEYCAPACITYFIXTURE000000000000000000001"
	keyFree    = "AFREFAKEACCOUNTKEYCAPACITYFIXTURE000000000000000000002"
	keyNoJWT   = "ANOJFAKEACCOUNTKEYCAPACITYFIXTURE000000000000000000003"
)

// limitsStore is 12 hours of epochs, 10 minutes apart, ending at last:
//   - nats-0 holds memory: 100 MiB growing 1 MiB an epoch, in 1 GiB;
//     nats-1 holds none and reports no memory limit.
//   - LIMITED's JWT allows 1000 GiB of file storage and no memory limit
//     (-1); two servers report its use, 100 GiB growing 1 GiB an epoch,
//     the larger read. Its stream open has no limit; bounded keeps 7 days.
//   - FREE's JWT sets no limit (-1); NOJWT has none. Each has a stream
//     without a limit.
func limitsStore(t *testing.T, last time.Time) *store.Store {
	t.Helper()
	st := emptyStore(t)
	r := rows{t, st}
	at := func(e int64) time.Time { return last.Add(-time.Duration(capEpochs-e) * capStep) }
	for i := range 2 {
		r.add("servers", map[string]any{"server_id": fmt.Sprintf("S%d", i), "server_name": fmt.Sprintf("nats-%d", i), "cluster": "c1",
			"jetstream": true, "first_epoch": 1, "last_epoch": capEpochs, "first_seen_at": at(1), "last_seen_at": at(capEpochs)})
	}
	for _, a := range []string{keyLimited, keyFree, keyNoJWT} {
		r.add("accounts", map[string]any{"account": a, "is_system": false, "first_epoch": 1, "last_epoch": capEpochs})
	}
	const gib, mib = int64(1 << 30), int64(1 << 20)
	for e := int64(1); e <= capEpochs; e++ {
		if _, err := st.DB().Exec(`INSERT INTO epochs VALUES (?, ?, ?, 0, 2, 2, ['nats-0', 'nats-1'], [], [], 0, false, [], NULL)`,
			e, at(e), at(e).Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		r.add("server_stats", map[string]any{"epoch": e, "server_id": "S0", "js_enabled": true, "js_storage": 10 * gib, "js_max_storage": 50 * gib,
			"js_memory": 100*mib + mib*e, "js_max_memory": gib})
		r.add("server_stats", map[string]any{"epoch": e, "server_id": "S1", "js_enabled": true, "js_storage": 10 * gib, "js_max_storage": 50 * gib,
			"js_memory": 0, "js_max_memory": 0})
		for i, v := range []int64{100*gib + gib*e, 99 * gib} {
			r.add("account_stats", map[string]any{"epoch": e, "server_id": fmt.Sprintf("S%d", i), "account": keyLimited, "js_storage": v, "js_memory": 0})
		}
		r.add("account_stats", map[string]any{"epoch": e, "server_id": "S0", "account": keyFree, "js_storage": gib * e, "js_memory": 0})
		for _, s := range []struct{ account, stream string }{{keyLimited, "open"}, {keyLimited, "bounded"}, {keyFree, "open"}, {keyNoJWT, "open"}} {
			r.add("stream_stats", map[string]any{"epoch": e, "server_id": "S0", "account": s.account, "stream": s.stream, "is_leader": true,
				"leader": "nats-0", "messages": 10 * e, "bytes": mib * e, "first_ts": at(e).Add(-time.Hour)})
		}
	}
	for _, s := range []struct {
		account, stream string
		maxAge          time.Duration
	}{{keyLimited, "open", 0}, {keyLimited, "bounded", 7 * day}, {keyFree, "open", 0}, {keyNoJWT, "open", 0}} {
		r.add("streams", map[string]any{"account": s.account, "stream": s.stream, "first_epoch": 1, "last_epoch": capEpochs})
		r.add("stream_config", map[string]any{"server_id": "S0", "account": s.account, "stream": s.stream, "first_epoch": 1, "last_epoch": capEpochs,
			"config_hash": "h", "retention": "limits", "storage": "file", "replicas": 1, "max_msgs": -1, "max_bytes": -1,
			"max_age_ns": int64(s.maxAge), "max_msgs_per_subject": -1, "discard": "old"})
	}
	lookup := func(acc string, cols map[string]any) {
		row := map[string]any{"epoch": int64(capEpochs), "server_id": "S0", "account": acc, "looked_up_at": last.Add(2 * time.Second),
			"is_system": false, "expired": false, "imports": 0, "exports": 0, "has_jwt": true}
		for k, v := range cols {
			row[k] = v
		}
		r.add("account_details", row)
	}
	lookup(keyLimited, map[string]any{"limit_js_disk": 1000 * gib, "limit_js_memory": -1})
	lookup(keyFree, map[string]any{"limit_js_disk": -1, "limit_js_memory": -1})
	lookup(keyNoJWT, map[string]any{"has_jwt": false})
	return st
}

// The store reads memory stores and account limits, and bounds a stream
// without its own limit by its account's, in a session time zone that is
// not UTC.
func TestCapacityLimitsFromStore(t *testing.T) {
	last := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	st := limitsStore(t, last)
	if _, err := st.DB().Exec(`SET GLOBAL TimeZone = 'Pacific/Honolulu'`); err != nil {
		t.Fatal(err)
	}
	clock := last.Add(30 * time.Second)
	_, c := apitest.Serve(t, &storeprov.Provider{St: st, Interval: capStep, Live: true, Clock: func() time.Time { return clock }, Retention: 14 * day})
	rng := &v1.TimeRange{Start: timestamppb.New(last.Add(-24 * time.Hour)), End: timestamppb.New(last.Add(time.Minute))}
	res := must(c.GetCapacity(ctx, connect.NewRequest(&v1.GetCapacityRequest{Range: rng})))
	const gib, mib = int64(1 << 30), int64(1 << 20)

	mem := map[string]*v1.ServerStore{}
	for _, m := range res.GetMemoryStores() {
		mem[m.GetServerName()] = m
	}
	// 1 MiB every 10 minutes: 144 MiB a day.
	if m := mem["nats-0"]; !m.GetReported() || m.GetUsed() != 100*mib+mib*capEpochs || m.GetLimit() != gib || m.GetPerDay() != float64(144*mib) ||
		m.DaysToFull == nil || m.GetFindingId() != "" {
		t.Errorf("nats-0 memory = %v", m)
	}
	if m := mem["nats-1"]; !m.GetReported() || m.GetUsed() != 0 || m.Limit != nil || m.DaysToFull != nil {
		t.Errorf("nats-1 memory = %v", m)
	}
	if len(res.GetServers()) != 2 || res.GetServers()[0].GetUsed() != 10*gib {
		t.Errorf("file stores = %v", res.GetServers())
	}

	if len(res.GetAccountStores()) != 1 {
		t.Fatalf("account stores = %v", res.GetAccountStores())
	}
	// LIMITED grows 1 GiB every 10 minutes: 144 GiB a day, the larger report.
	a := res.GetAccountStores()[0]
	used := 100*gib + gib*capEpochs
	if a.GetAccount() != keyLimited || a.GetStorage() != "file" || a.GetUsed() != used || a.GetLimit() != 1000*gib ||
		a.GetPerDay() != float64(144*gib) || a.GetDaysToFull() != float64(1000*gib-used)/float64(144*gib) {
		t.Errorf("LIMITED = %v", a)
	}

	type key struct{ account, stream string }
	by := map[key]*v1.CapacityStream{}
	for _, s := range res.GetStreams() {
		by[key{s.GetAccount(), s.GetStream()}] = s
	}
	open := by[key{keyLimited, "open"}]
	if open.GetAccountLimit() != 1000*gib || open.GetNextLimit().GetLimit() != v1.NextLimitKind_NEXT_LIMIT_KIND_ACCOUNT ||
		open.GetNextLimit().GetState() != v1.NextLimitState_NEXT_LIMIT_STATE_REACHES || open.GetNextLimit().GetDays() != a.GetDaysToFull() ||
		open.GetStatus() != v1.CapacityStatus_CAPACITY_STATUS_UNBOUNDED {
		t.Errorf("LIMITED open = %v", open)
	}
	if b := by[key{keyLimited, "bounded"}]; b.GetAccountLimit() != 1000*gib || b.GetNextLimit().GetLimit() != v1.NextLimitKind_NEXT_LIMIT_KIND_ACCOUNT {
		t.Errorf("LIMITED bounded = %v: the account fills in %.1f days, before its 7 days", b, a.GetDaysToFull())
	}
	for _, acc := range []string{keyFree, keyNoJWT} {
		if s := by[key{acc, "open"}]; s.AccountLimit != nil || s.GetNextLimit().GetLimit() != v1.NextLimitKind_NEXT_LIMIT_KIND_NONE {
			t.Errorf("%s open = %v", acc, s)
		}
	}
}
