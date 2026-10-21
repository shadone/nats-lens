package fake_test

import (
	"testing"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// capacityOf reads a demo system's Capacity page for the default range.
func capacityOf(t *testing.T, system string) (*v1.GetCapacityResponse, map[string]*v1.CapacityStream, map[string]*v1.ServerStore) {
	t.Helper()
	res := must(demo(t).GetCapacity(ctx, connect.NewRequest(&v1.GetCapacityRequest{System: system})))
	streams, servers := map[string]*v1.CapacityStream{}, map[string]*v1.ServerStore{}
	for _, s := range res.GetStreams() {
		streams[s.GetStream()] = s
	}
	for _, s := range res.GetServers() {
		servers[s.GetServerName()] = s
	}
	return res, streams, servers
}

type limitWant struct {
	limit v1.NextLimitKind
	state v1.NextLimitState
}

func checkStreams(t *testing.T, system string, by map[string]*v1.CapacityStream, status map[string]v1.CapacityStatus, next map[string]limitWant) {
	t.Helper()
	for name, st := range status {
		if by[name].GetStatus() != st {
			t.Errorf("%s %s: status %v, want %v", system, name, by[name].GetStatus(), st)
		}
	}
	for name, w := range next {
		if n := by[name].GetNextLimit(); n.GetLimit() != w.limit || n.GetState() != w.state {
			t.Errorf("%s %s: next limit %v, want %v", system, name, n, w)
		}
	}
}

// example-prod: three cards a few percent full that fill in more than 5
// years; a stream of every kind; order-archive awaiting a decision first,
// then audit-archive filling under its mute; KV_customer-config accepted.
func TestDemoCapacityProd(t *testing.T) {
	res, by, servers := capacityOf(t, fake.Cluster)
	if len(servers) != 3 || res.GetStreamsTotal() != 13 || res.GetStreams()[0].GetStream() != "order-archive" || res.GetStreams()[1].GetStream() != "audit-archive" {
		t.Fatalf("prod: %d servers, %d streams, first %v", len(servers), res.GetStreamsTotal(), res.GetStreams()[:2])
	}
	for _, s := range servers {
		share := float64(s.GetUsed()) / float64(s.GetLimit())
		if !s.GetReported() || share > 0.05 || s.GetDaysToFull() < 5*365 || s.GetFindingId() != "" {
			t.Errorf("%s: %.3f full, %v days", s.GetServerName(), share, s.DaysToFull)
		}
	}
	checkStreams(t, fake.Cluster, by, map[string]v1.CapacityStatus{
		"order-archive": v1.CapacityStatus_CAPACITY_STATUS_AWAITING_DECISION, "KV_customer-config": v1.CapacityStatus_CAPACITY_STATUS_ACCEPTED,
		"audit-archive": v1.CapacityStatus_CAPACITY_STATUS_FILLS_SOON, "order-jobs": v1.CapacityStatus_CAPACITY_STATUS_BOUNDED,
		"KV_order-jobs-cache": v1.CapacityStatus_CAPACITY_STATUS_BOUNDED, "OBJ_image-input": v1.CapacityStatus_CAPACITY_STATUS_BOUNDED,
	}, map[string]limitWant{
		"order-jobs":         {v1.NextLimitKind_NEXT_LIMIT_KIND_AGE, v1.NextLimitState_NEXT_LIMIT_STATE_REACHES},
		"catalog-changes":    {v1.NextLimitKind_NEXT_LIMIT_KIND_AGE, v1.NextLimitState_NEXT_LIMIT_STATE_STEADY},
		"order-callbacks":    {v1.NextLimitKind_NEXT_LIMIT_KIND_EMPTY, 0},
		"order-archive":      {v1.NextLimitKind_NEXT_LIMIT_KIND_NONE, 0},
		"audit-archive":      {v1.NextLimitKind_NEXT_LIMIT_KIND_BYTES, v1.NextLimitState_NEXT_LIMIT_STATE_REACHES},
		"KV_customer-config": {v1.NextLimitKind_NEXT_LIMIT_KIND_PER_SUBJECT, v1.NextLimitState_NEXT_LIMIT_STATE_NOT_FORECAST},
		"OBJ_image-input":    {v1.NextLimitKind_NEXT_LIMIT_KIND_AGE, v1.NextLimitState_NEXT_LIMIT_STATE_STEADY},
	})
	if s := by["KV_customer-config"]; s.GetMute().GetMutedBy() != "ops@example.com" || s.MaxBytes != nil || s.MaxAge != nil || s.GetKeys() != 41 {
		t.Errorf("KV_customer-config = %v", s)
	}
	if s := by["audit-archive"]; s.GetFindingDays() == 0 || !s.GetNextLimit().GetDiscardNew() || s.GetMaxMsgsPerSubject() != 100_000_000 {
		t.Errorf("audit-archive = %v", s)
	}
	if s := by["order-callbacks"]; s.GetRetention() != "workqueue" || s.FirstTs != nil {
		t.Errorf("order-callbacks = %v", s)
	}
	if by["KV_worker-presence"].GetStorage() != "memory" || by["media-windows"].GetRetention() != "interest" {
		t.Error("prod lacks a memory bucket or an interest stream")
	}
}

// example-global: nats-ap-2 fills by server-014, nats-us-4 did not
// report; order-archive (a mirror) is muted, KV_feature-flags awaits a
// decision again after its acceptance ended, KV_session-cache is bounded
// by its writers' TTLs and shrinks, price-changes refuses messages.
func TestDemoCapacityGlobal(t *testing.T) {
	res, by, servers := capacityOf(t, fake.GlobalSystem)
	if len(servers) != 24 || res.GetServers()[0].GetServerName() != "nats-ap-2" {
		t.Fatalf("global: %d servers, first %s", len(servers), res.GetServers()[0].GetServerName())
	}
	if s := servers["nats-ap-2"]; s.GetFindingCode() != "server-014" || s.GetFindingDays() == 0 || s.GetDaysToFull() > 60 || s.GetDaysToFull() < 14 {
		t.Errorf("nats-ap-2 = %v", s)
	}
	if s := servers["nats-us-4"]; s.GetReported() || s.Used != nil {
		t.Errorf("nats-us-4 = %v", s)
	}
	checkStreams(t, fake.GlobalSystem, by, map[string]v1.CapacityStatus{
		"order-archive": v1.CapacityStatus_CAPACITY_STATUS_MUTED, "KV_feature-flags": v1.CapacityStatus_CAPACITY_STATUS_AWAITING_DECISION,
		"KV_session-cache": v1.CapacityStatus_CAPACITY_STATUS_DECLARED, "price-changes": v1.CapacityStatus_CAPACITY_STATUS_BOUNDED,
	}, map[string]limitWant{
		"price-changes":      {v1.NextLimitKind_NEXT_LIMIT_KIND_MESSAGES, v1.NextLimitState_NEXT_LIMIT_STATE_REACHED_REFUSING},
		"OBJ_product-images": {v1.NextLimitKind_NEXT_LIMIT_KIND_BYTES, v1.NextLimitState_NEXT_LIMIT_STATE_REACHES},
		"shipping-updates":   {v1.NextLimitKind_NEXT_LIMIT_KIND_MESSAGES, v1.NextLimitState_NEXT_LIMIT_STATE_REACHES},
	})
	if s := by["order-archive"]; s.GetMirror() != "order-events" || s.GetMute().GetExpiresAt() == nil || s.GetBytesPerDay() < 1<<30 {
		t.Errorf("order-archive = %v", s)
	}
	if s := by["KV_session-cache"]; s.GetDeclaredReason() != api.DeclaredWriterTTLs || s.GetBytesPerDay() >= 0 {
		t.Errorf("KV_session-cache = %v", s)
	}
	if s := by["KV_feature-flags"]; s.GetMute() != nil || s.GetBytesPerDay() < 1.5*(1<<20) {
		t.Errorf("KV_feature-flags = %v", s)
	}
	if by["search-index"].GetSources() != 2 {
		t.Errorf("search-index = %v", by["search-index"])
	}
}

// example-staging: under 6 hours of data, so no growth anywhere;
// catalog-changes awaits a decision and order-jobs has not reached its
// message limit.
func TestDemoCapacityStaging(t *testing.T) {
	res, by, servers := capacityOf(t, fake.StagingSystem)
	if res.GetEpoch() == 0 || len(servers) != 3 {
		t.Fatalf("staging = %v", res)
	}
	for _, s := range servers {
		if s.PerDay != nil || s.DaysToFull != nil || !s.GetReported() {
			t.Errorf("%s = %v", s.GetServerName(), s)
		}
	}
	for _, s := range by {
		if s.BytesPerDay != nil || s.MsgsPerDay != nil {
			t.Errorf("%s grows %v", s.GetStream(), s.GetBytesPerDay())
		}
	}
	checkStreams(t, fake.StagingSystem, by, map[string]v1.CapacityStatus{
		"catalog-changes": v1.CapacityStatus_CAPACITY_STATUS_AWAITING_DECISION,
	}, map[string]limitWant{
		"order-jobs": {v1.NextLimitKind_NEXT_LIMIT_KIND_MESSAGES, v1.NextLimitState_NEXT_LIMIT_STATE_NOT_REACHED},
	})
}
