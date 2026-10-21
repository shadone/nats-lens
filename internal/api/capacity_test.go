package api_test

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

var capAt = time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)

func f64(v float64) *float64             { return &v }
func age(d time.Duration) *time.Duration { return &d }
func since(d time.Duration) *time.Time {
	t := capAt.Add(-d)
	return &t
}

const day = 24 * time.Hour

// A range without an epoch gives the empty response.
func TestFinishCapacityNoEpoch(t *testing.T) {
	for _, f := range []*api.CapacityFacts{nil, {}, {Streams: []api.CapacityStreamFact{{Account: "APP", Stream: "s"}}}} {
		if res := api.FinishCapacity(f); res.GetEpoch() != 0 || res.GetEpochAt() != nil || len(res.GetStreams()) != 0 || res.GetGrowthFrom() != nil {
			t.Errorf("FinishCapacity(%v) = %v", f, res)
		}
	}
}

// Each next-limit state from the stream's limits, numbers and growth.
func TestNextLimit(t *testing.T) {
	cases := []struct {
		name  string
		s     api.CapacityStreamFact
		limit v1.NextLimitKind
		state v1.NextLimitState
		days  *float64
		dnew  bool
	}{
		{"empty", api.CapacityStreamFact{Messages: i64(0), MaxAge: age(day)}, v1.NextLimitKind_NEXT_LIMIT_KIND_EMPTY, 0, nil, false},
		{"no limit", api.CapacityStreamFact{Messages: i64(5), MaxMsgs: i64(-1), MaxBytes: i64(-1), MaxAge: age(0), FirstTS: since(day)},
			v1.NextLimitKind_NEXT_LIMIT_KIND_NONE, 0, nil, false},
		{"age at 95%", api.CapacityStreamFact{Messages: i64(5), MaxAge: age(30 * day), FirstTS: since(28*day + 12*time.Hour)},
			v1.NextLimitKind_NEXT_LIMIT_KIND_AGE, v1.NextLimitState_NEXT_LIMIT_STATE_STEADY, nil, false},
		{"age at 95% with discard new is still steady", api.CapacityStreamFact{Messages: i64(5), Discard: "new", MaxAge: age(30 * day),
			FirstTS: since(30 * day)}, v1.NextLimitKind_NEXT_LIMIT_KIND_AGE, v1.NextLimitState_NEXT_LIMIT_STATE_STEADY, nil, false},
		{"age below 95% reaches", api.CapacityStreamFact{Messages: i64(5), MaxAge: age(7 * day), FirstTS: since(4 * day)},
			v1.NextLimitKind_NEXT_LIMIT_KIND_AGE, v1.NextLimitState_NEXT_LIMIT_STATE_REACHES, f64(3), false},
		{"messages at 99% discard old", api.CapacityStreamFact{Messages: i64(990), MaxMsgs: i64(1000), FirstTS: since(day)},
			v1.NextLimitKind_NEXT_LIMIT_KIND_MESSAGES, v1.NextLimitState_NEXT_LIMIT_STATE_STEADY, nil, false},
		{"messages below 99% not reached without growth", api.CapacityStreamFact{Messages: i64(989), MaxMsgs: i64(1000), FirstTS: since(day)},
			v1.NextLimitKind_NEXT_LIMIT_KIND_MESSAGES, v1.NextLimitState_NEXT_LIMIT_STATE_NOT_REACHED, nil, false},
		{"bytes at 99% discard new refuses, before an age at its limit", api.CapacityStreamFact{Messages: i64(5), Bytes: i64(99), MaxBytes: i64(100),
			Discard: "new", MaxAge: age(day), FirstTS: since(day)},
			v1.NextLimitKind_NEXT_LIMIT_KIND_BYTES, v1.NextLimitState_NEXT_LIMIT_STATE_REACHED_REFUSING, nil, true},
		{"the soonest forecast", api.CapacityStreamFact{Messages: i64(100), Bytes: i64(1000), MaxMsgs: i64(1100), MaxBytes: i64(2000),
			MsgsPerDay: f64(100), BytesPerDay: f64(500), Discard: "new", MaxAge: age(30 * day), FirstTS: since(day)},
			v1.NextLimitKind_NEXT_LIMIT_KIND_BYTES, v1.NextLimitState_NEXT_LIMIT_STATE_REACHES, f64(2), true},
		{"shrinking: not reached", api.CapacityStreamFact{Messages: i64(100), Bytes: i64(1000), MaxBytes: i64(2000), BytesPerDay: f64(-50),
			FirstTS: since(day)}, v1.NextLimitKind_NEXT_LIMIT_KIND_BYTES, v1.NextLimitState_NEXT_LIMIT_STATE_NOT_REACHED, nil, false},
		{"per subject only", api.CapacityStreamFact{Messages: i64(5), MaxMsgsPerSubject: i64(5), MaxAge: age(0), FirstTS: since(day)},
			v1.NextLimitKind_NEXT_LIMIT_KIND_PER_SUBJECT, v1.NextLimitState_NEXT_LIMIT_STATE_NOT_FORECAST, nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.s.Account, c.s.Stream = "APP", "s"
			res := api.FinishCapacity(&api.CapacityFacts{Epoch: 1, EpochAt: capAt, Streams: []api.CapacityStreamFact{c.s}})
			n := res.GetStreams()[0].GetNextLimit()
			if n.GetLimit() != c.limit || n.GetState() != c.state || n.GetDiscardNew() != c.dnew || (n.Days == nil) != (c.days == nil) ||
				(c.days != nil && fmt.Sprintf("%.3f", n.GetDays()) != fmt.Sprintf("%.3f", *c.days)) {
				t.Errorf("next limit = %v, want %v %v days %v discard new %v", n, c.limit, c.state, c.days, c.dnew)
			}
		})
	}
}

func firingFinding(code, key string, days *float64, mute *v1.Mute) *v1.Finding {
	f := &v1.Finding{Id: code + ":" + key, CheckCode: code, State: v1.FindingState_FINDING_STATE_FIRING, Severity: v1.Severity_SEVERITY_WARNING,
		Entity: &v1.EntityRef{Key: key}, Mute: mute}
	if days != nil {
		f.Value = &v1.FindingValue{Number: days}
	}
	return f
}

// Each status from the findings, their mutes and the inventory; the
// order puts fills soon first, then awaiting a decision, then the size
// times replicas.
func TestCapacityStatusAndOrder(t *testing.T) {
	unb := func(name string, bytes int64, declared string) api.CapacityStreamFact {
		return api.CapacityStreamFact{Account: "APP", Stream: name, Messages: i64(1), Bytes: i64(bytes), Replicas: 3, MaxMsgs: i64(-1),
			MaxBytes: i64(-1), MaxAge: age(0), Declared: declared, FirstTS: since(day)}
	}
	bounded := func(name string, bytes int64, replicas int32) api.CapacityStreamFact {
		s := unb(name, bytes, "")
		s.MaxAge, s.Replicas = age(7*day), replicas
		return s
	}
	f := &api.CapacityFacts{Epoch: 1, EpochAt: capAt, Streams: []api.CapacityStreamFact{
		bounded("small", 10, 1), bounded("big", 100, 1), bounded("wide", 40, 3),
		unb("accepted", 1, ""), unb("muted", 1, ""), unb("awaiting", 1, ""), unb("declared", 1, api.DeclaredInventory), unb("unbounded", 1, ""),
		unb("declared-but-fires", 1, api.DeclaredWriterTTLs), bounded("soon", 1, 1), bounded("sooner", 1, 1), unb("awaiting-big", 500, ""),
	}, Findings: []*v1.Finding{
		firingFinding("stream-005", "APP/accepted", nil, &v1.Mute{Accepted: true, MutedBy: "ops"}),
		firingFinding("stream-005", "APP/muted", nil, &v1.Mute{MutedBy: "ops"}),
		firingFinding("stream-005", "APP/awaiting", nil, nil),
		firingFinding("stream-005", "APP/awaiting-big", nil, nil),
		firingFinding("stream-005", "APP/declared-but-fires", nil, nil),
		firingFinding("stream-006", "APP/soon", f64(9), &v1.Mute{MutedBy: "ops"}),
		firingFinding("stream-006", "APP/sooner", f64(2), nil),
		{Id: "stream-005:APP/unbounded", CheckCode: "stream-005", State: v1.FindingState_FINDING_STATE_RESOLVED, Entity: &v1.EntityRef{Key: "APP/unbounded"}},
	}}
	res := api.FinishCapacity(f)
	status := map[string]*v1.CapacityStream{}
	var order []string
	for _, s := range res.GetStreams() {
		status[s.GetStream()] = s
		order = append(order, s.GetStream())
	}
	want := map[string]v1.CapacityStatus{
		"small": v1.CapacityStatus_CAPACITY_STATUS_BOUNDED, "accepted": v1.CapacityStatus_CAPACITY_STATUS_ACCEPTED,
		"muted": v1.CapacityStatus_CAPACITY_STATUS_MUTED, "awaiting": v1.CapacityStatus_CAPACITY_STATUS_AWAITING_DECISION,
		"declared": v1.CapacityStatus_CAPACITY_STATUS_DECLARED, "unbounded": v1.CapacityStatus_CAPACITY_STATUS_UNBOUNDED,
		"declared-but-fires": v1.CapacityStatus_CAPACITY_STATUS_AWAITING_DECISION, "soon": v1.CapacityStatus_CAPACITY_STATUS_FILLS_SOON,
	}
	for name, st := range want {
		if status[name].GetStatus() != st {
			t.Errorf("%s: status %v, want %v", name, status[name].GetStatus(), st)
		}
	}
	if s := status["soon"]; s.GetFindingId() != "stream-006:APP/soon" || s.GetFindingDays() != 9 || s.GetMute().GetMutedBy() != "ops" {
		t.Errorf("soon = %v", s)
	}
	if s := status["accepted"]; s.GetFindingId() != "stream-005:APP/accepted" || !s.GetMute().GetAccepted() || s.FindingDays != nil {
		t.Errorf("accepted = %v", s)
	}
	if s := status["declared"]; s.GetDeclaredReason() != api.DeclaredInventory || s.GetFindingId() != "" {
		t.Errorf("declared = %v", s)
	}
	if status["unbounded"].GetDeclaredReason() != "" || status["declared-but-fires"].GetDeclaredReason() != "" {
		t.Error("a declared reason on a stream that is not declared")
	}
	if s := status["small"]; s.MaxMsgs != nil || s.MaxBytes != nil || s.GetMaxAge().AsDuration() != 7*day {
		t.Errorf("limits not set are unset: %v", s)
	}
	// A muted forecast ("soon") sorts after the rows awaiting a decision.
	head := []string{"sooner", "awaiting-big", "awaiting", "declared-but-fires", "soon", "wide", "big", "small"}
	if !slices.Equal(order[:len(head)], head) {
		t.Errorf("order %v, want it to start %v", order, head)
	}
}

// Keys are read for KV buckets only; the kind follows the name's prefix.
func TestCapacityKindAndKeys(t *testing.T) {
	f := &api.CapacityFacts{Epoch: 1, EpochAt: capAt, Streams: []api.CapacityStreamFact{
		{Account: "APP", Stream: "KV_a", Keys: i64(4)}, {Account: "APP", Stream: "OBJ_b", Keys: i64(4)}, {Account: "APP", Stream: "c", Keys: i64(4)},
	}}
	for _, s := range api.FinishCapacity(f).GetStreams() {
		want := map[string]v1.CapacityStreamKind{"KV_a": v1.CapacityStreamKind_CAPACITY_STREAM_KIND_KEY_VALUE,
			"OBJ_b": v1.CapacityStreamKind_CAPACITY_STREAM_KIND_OBJECT_STORE, "c": v1.CapacityStreamKind_CAPACITY_STREAM_KIND_STREAM}[s.GetStream()]
		if s.GetKind() != want || (s.Keys != nil) != (s.GetStream() == "KV_a") {
			t.Errorf("%s: kind %v keys %v", s.GetStream(), s.GetKind(), s.Keys)
		}
	}
}

// At most 500 streams are listed, with the total.
func TestFinishCapacityLimit(t *testing.T) {
	f := &api.CapacityFacts{Epoch: 1, EpochAt: capAt}
	for i := range 503 {
		f.Streams = append(f.Streams, api.CapacityStreamFact{Account: "APP", Stream: fmt.Sprintf("s%03d", i)})
	}
	res := api.FinishCapacity(f)
	if len(res.GetStreams()) != api.MaxCapacityRows || res.GetStreamsTotal() != 503 {
		t.Errorf("%d streams, total %d", len(res.GetStreams()), res.GetStreamsTotal())
	}
}

// Time to full needs a growth above 0, a known limit and use; a server
// that did not report has neither; the finding is server-014's before
// server-007's, with its days. Cards group by cluster, then the soonest
// full, then the fullest.
func TestCapacityServers(t *testing.T) {
	f := &api.CapacityFacts{Epoch: 1, EpochAt: capAt, Servers: []api.ServerStoreFact{
		{Name: "b-slow", Cluster: "b", Reported: true, Used: i64(10), Limit: i64(110), PerDay: f64(1)},
		{Name: "b-fast", Cluster: "b", Reported: true, Used: i64(10), Limit: i64(110), PerDay: f64(10)},
		{Name: "b-full", Cluster: "b", Reported: true, Used: i64(90), Limit: i64(100), PerDay: f64(-1)},
		{Name: "b-empty", Cluster: "b", Reported: true, Used: i64(1), Limit: i64(100)},
		{Name: "a-nolimit", Cluster: "a", Reported: true, Used: i64(5), Limit: i64(0), PerDay: f64(1)},
		{Name: "a-gone", Cluster: "a", PerDay: f64(3)},
	}, Findings: []*v1.Finding{
		firingFinding("server-007", "b-full", f64(0.9), nil),
		firingFinding("server-007", "b-fast", f64(0.8), nil),
		firingFinding("server-014", "b-fast", f64(12.5), nil),
	}}
	res := api.FinishCapacity(f)
	var order []string
	by := map[string]*v1.ServerStore{}
	for _, s := range res.GetServers() {
		order = append(order, s.GetServerName())
		by[s.GetServerName()] = s
	}
	if want := []string{"a-gone", "a-nolimit", "b-fast", "b-slow", "b-full", "b-empty"}; !slices.Equal(order, want) {
		t.Errorf("order %v, want %v", order, want)
	}
	if s := by["b-fast"]; s.GetDaysToFull() != 10 || s.GetFindingCode() != "server-014" || s.GetFindingDays() != 12.5 {
		t.Errorf("b-fast = %v", s)
	}
	if s := by["b-full"]; s.DaysToFull != nil || s.GetFindingCode() != "server-007" || s.FindingDays != nil {
		t.Errorf("b-full = %v", s)
	}
	if s := by["a-nolimit"]; s.Limit != nil || s.DaysToFull != nil || s.GetUsed() != 5 {
		t.Errorf("a-nolimit = %v", s)
	}
	// Not reported: no rate from earlier collections either.
	if s := by["a-gone"]; s.GetReported() || s.Used != nil || s.Limit != nil || s.DaysToFull != nil || s.PerDay != nil {
		t.Errorf("a-gone = %v", s)
	}
	if by["b-empty"].PerDay != nil || by["b-empty"].DaysToFull != nil {
		t.Errorf("b-empty = %v", by["b-empty"])
	}
}

// Stores whose days to full differ only by floating-point noise keep their
// name order.
func TestCapacityServersTieOnNoise(t *testing.T) {
	f := &api.CapacityFacts{Epoch: 1, EpochAt: capAt, Servers: []api.ServerStoreFact{
		{Name: "nats-0", Cluster: "c", Reported: true, Used: i64(2076180480), Limit: i64(53687091200), PerDay: f64(17825792)},
		{Name: "nats-1", Cluster: "c", Reported: true, Used: i64(2076180480), Limit: i64(53687091200), PerDay: f64(17825792.00001)},
		{Name: "nats-2", Cluster: "c", Reported: true, Used: i64(2076180480), Limit: i64(53687091200), PerDay: f64(17825792.00002)},
	}}
	var order []string
	for _, s := range api.FinishCapacity(f).GetServers() {
		order = append(order, s.GetServerName())
	}
	if want := []string{"nats-0", "nats-1", "nats-2"}; !slices.Equal(order, want) {
		t.Errorf("order %v, want %v", order, want)
	}
}
