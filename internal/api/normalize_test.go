package api

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

var now = time.Date(2026, 10, 17, 14, 30, 0, 0, time.UTC)

func tsp(t time.Time) *timestamppb.Timestamp { return timestamppb.New(t) }

func TestResolveRange(t *testing.T) {
	r, err := ResolveRange(nil, now, DefaultRange)
	if err != nil || !r.GetEnd().AsTime().Equal(now) || !r.GetStart().AsTime().Equal(now.Add(-DefaultRange)) {
		t.Fatalf("unset range = %v, %v", r, err)
	}
	r, err = ResolveRange(&v1.TimeRange{End: tsp(now.Add(-time.Hour))}, now, time.Hour)
	if err != nil || !r.GetStart().AsTime().Equal(now.Add(-2*time.Hour)) {
		t.Fatalf("end only = %v, %v", r, err)
	}
	if _, err := ResolveRange(&v1.TimeRange{Start: tsp(now.Add(-MaxRange))}, now, time.Hour); err != nil {
		t.Fatalf("exactly MaxRange: %v", err)
	}
	bad := map[string]*v1.TimeRange{
		"start after end":     {Start: tsp(now), End: tsp(now.Add(-time.Second))},
		"empty":               {Start: tsp(now), End: tsp(now)},
		"longer than max":     {Start: tsp(now.Add(-MaxRange - time.Second)), End: tsp(now)},
		"8000 years":          {Start: tsp(time.Date(1, 1, 2, 0, 0, 0, 0, time.UTC)), End: tsp(time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC))},
		"invalid timestamp":   {Start: &timestamppb.Timestamp{Seconds: 1, Nanos: -1}},
		"beyond year 9999":    {End: &timestamppb.Timestamp{Seconds: 1 << 40}},
		"start after now end": {Start: tsp(now.Add(time.Hour))},
	}
	for name, r := range bad {
		if _, err := ResolveRange(r, now, DefaultRange); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestStepAndBuckets(t *testing.T) {
	day := &v1.TimeRange{Start: tsp(now.Add(-24 * time.Hour)), End: tsp(now)}
	cases := []struct {
		r    *v1.TimeRange
		n    int32
		min  time.Duration
		want time.Duration
	}{
		{day, 96, 30 * time.Second, 15 * time.Minute},
		{day, 1, time.Second, 24 * time.Hour},
		{day, 0, time.Second, 24 * time.Hour}, // a zero count is treated as one
		{day, 2000, time.Minute, time.Minute}, // never below the interval
		{&v1.TimeRange{Start: tsp(now.Add(-1001 * time.Millisecond)), End: tsp(now)}, 1, 0, 2 * time.Second}, // rounded up to seconds
		{&v1.TimeRange{Start: tsp(now.Add(-time.Hour - time.Nanosecond)), End: tsp(now)}, 60, 0, 61 * time.Second},
	}
	for i, c := range cases {
		if got := Step(c.r, c.n, c.min); got != c.want {
			t.Errorf("case %d: step = %v, want %v", i, got, c.want)
		}
	}
	if got := BucketStart(time.Date(2026, 10, 17, 14, 37, 12, 0, time.UTC), 15*time.Minute); !got.Equal(time.Date(2026, 10, 17, 14, 30, 0, 0, time.UTC)) {
		t.Errorf("bucket start = %v", got)
	}
	if got := BucketStart(time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC), time.Hour); !got.Equal(time.Date(9999, 12, 31, 23, 0, 0, 0, time.UTC)) {
		t.Errorf("bucket start after 2262 = %v", got)
	}
	if got := BucketStart(time.Unix(-1, 0), time.Minute); !got.Equal(time.Unix(-60, 0)) {
		t.Errorf("bucket start before 1970 = %v", got)
	}
	// 2000.5 s of 1 s buckets is 2001 buckets, the last one partial.
	tl := &v1.GetStatusTimelineRequest{Range: &v1.TimeRange{Start: tsp(now.Add(-2000*time.Second - 500*time.Millisecond)), End: tsp(now)},
		Bucket: durationpb.New(time.Second)}
	if err := normalizeTimeline(tl, now); !errors.Is(err, ErrInvalid) {
		t.Errorf("2001 buckets: %v", err)
	}
	tl.Range = &v1.TimeRange{Start: tsp(now.Add(-2000 * time.Second)), End: tsp(now)}
	if err := normalizeTimeline(tl, now); err != nil {
		t.Errorf("2000 buckets: %v", err)
	}
}

func TestPageAndCursor(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}
	p, next, err := Page(items, "", 2)
	if err != nil || len(p) != 2 || next == "" {
		t.Fatalf("first page %v %q %v", p, next, err)
	}
	p, next, _ = Page(items, next, 2)
	p, next, _ = Page(items, next, 2)
	if len(p) != 1 || p[0] != 5 || next != "" {
		t.Fatalf("last page %v %q", p, next)
	}
	if p, next, err := Page(items, EncodeCursor(Cursor{Offset: 9}), 2); err != nil || len(p) != 0 || next != "" {
		t.Fatalf("past the end %v %q %v", p, next, err)
	}
	c := Cursor{Time: now, ID: 42, Epoch: 7}
	back, err := DecodeCursor(EncodeCursor(c))
	if err != nil || !back.Time.Equal(now) || back.ID != 42 || back.Epoch != 7 {
		t.Fatalf("round trip %+v %v", back, err)
	}
	for _, tok := range []string{"%%%", "e30x", raw(`{"o":-1}`), raw(`{"o":2097152}`), raw(`{"o":9223372036854775807}`),
		raw(`{"t":"garbage"}`), raw(`{"i":-5}`), raw(`[]`)} {
		if _, err := DecodeCursor(tok); !errors.Is(err, ErrInvalid) {
			t.Errorf("token %q: %v", tok, err)
		}
	}
}

// raw encodes s as a page token without checking it.
func raw(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }

func TestNormalizeMute(t *testing.T) {
	good := func() *v1.MuteFindingRequest {
		return &v1.MuteFindingRequest{CheckCode: "stream-031", Reason: "  in review  ", MutedBy: " op ",
			ExpiresAt: tsp(now.Add(time.Hour + 500*time.Millisecond))}
	}
	m := good()
	if err := normalizeMute(m, now, ""); err != nil {
		t.Fatal(err)
	}
	if m.GetReason() != "in review" || m.GetMutedBy() != "op" || !m.GetExpiresAt().AsTime().Equal(now.Add(time.Hour)) {
		t.Fatalf("normalized = %v", m)
	}
	m = good()
	m.MutedBy = ""
	if err := normalizeMute(m, now, "iap@example.com"); err != nil || m.GetMutedBy() != "iap@example.com" {
		t.Fatalf("IAP user: %v %v", m.GetMutedBy(), err)
	}
	m = good()
	m.ExpiresAt = tsp(now.Add(MaxMuteLength))
	if err := normalizeMute(m, now, ""); err != nil {
		t.Fatalf("90 days: %v", err)
	}
	bad := map[string]func(*v1.MuteFindingRequest){
		"check code without digits": func(m *v1.MuteFindingRequest) { m.CheckCode = "stream" },
		"check code upper case":     func(m *v1.MuteFindingRequest) { m.CheckCode = "Stream-031" },
		"check code four digits":    func(m *v1.MuteFindingRequest) { m.CheckCode = "stream-0311" },
		"blank reason":              func(m *v1.MuteFindingRequest) { m.Reason = "   " },
		"long reason":               func(m *v1.MuteFindingRequest) { m.Reason = strings.Repeat("a", MaxReasonLength+1) },
		"long name":                 func(m *v1.MuteFindingRequest) { m.MutedBy = strings.Repeat("a", MaxNameLength+1) },
		"no user":                   func(m *v1.MuteFindingRequest) { m.MutedBy = "" },
		"no expiry":                 func(m *v1.MuteFindingRequest) { m.ExpiresAt = nil },
		"expiry under a minute":     func(m *v1.MuteFindingRequest) { m.ExpiresAt = tsp(now.Add(59*time.Second + 999*time.Millisecond)) },
		"expiry after 90 days":      func(m *v1.MuteFindingRequest) { m.ExpiresAt = tsp(now.Add(MaxMuteLength + time.Second)) },
		"unknown entity type":       func(m *v1.MuteFindingRequest) { m.Entity = &v1.EntityRef{Type: 42, Key: "k"} },
		"unspecified entity":        func(m *v1.MuteFindingRequest) { m.Entity = &v1.EntityRef{Key: "k"} },
		"long entity key": func(m *v1.MuteFindingRequest) {
			m.Entity = &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_CLIENT, Key: strings.Repeat("k", 257)}
		},
		"consumer key with 2 parts": func(m *v1.MuteFindingRequest) {
			m.Entity = &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_CONSUMER, Key: "A/s"}
		},
	}
	for name, change := range bad {
		m := good()
		change(m)
		if err := normalizeMute(m, now, ""); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestCheckEntity(t *testing.T) {
	ok := []*v1.EntityRef{
		nil,
		{},
		{Type: v1.EntityType_ENTITY_TYPE_SYSTEM},
		{Type: v1.EntityType_ENTITY_TYPE_SERVER, Key: "nats-0"},
		{Type: v1.EntityType_ENTITY_TYPE_STREAM, Key: "A/s"},
		{Type: v1.EntityType_ENTITY_TYPE_CONSUMER, Key: "A/s/c"},
		{Type: v1.EntityType_ENTITY_TYPE_CONNECTION, Key: "nats-0/17"},
		{Type: v1.EntityType_ENTITY_TYPE_CLIENT, Key: "app/worker"},
	}
	for _, e := range ok {
		if err := checkEntity("entity", e, true); err != nil {
			t.Errorf("%v: %v", e, err)
		}
	}
	bad := []*v1.EntityRef{
		{Key: "x"},
		{Type: 99, Key: "x"},
		{Type: v1.EntityType_ENTITY_TYPE_SYSTEM, Key: "x"},
		{Type: v1.EntityType_ENTITY_TYPE_SERVER},
		{Type: v1.EntityType_ENTITY_TYPE_SERVER, Key: "a/b"},
		{Type: v1.EntityType_ENTITY_TYPE_STREAM, Key: "A/"},
		{Type: v1.EntityType_ENTITY_TYPE_CONNECTION, Key: "nats-0/x"},
		{Type: v1.EntityType_ENTITY_TYPE_CONNECTION, Key: "nats-0/-1"},
	}
	for _, e := range bad {
		if err := checkEntity("entity", e, true); !errors.Is(err, ErrInvalid) {
			t.Errorf("%v: %v", e, err)
		}
	}
	if err := checkEntity("entity", nil, false); !errors.Is(err, ErrInvalid) {
		t.Errorf("required entity unset: %v", err)
	}
}

// Buckets near the largest Duration count as one bucket instead of
// overflowing into a negative count.
func TestBucketOverflow(t *testing.T) {
	for _, secs := range []int64{9223372035, 9223372036, 315576000000} {
		req := &v1.GetStatusTimelineRequest{Bucket: &durationpb.Duration{Seconds: secs}}
		if err := normalizeTimeline(req, now); err != nil {
			t.Fatalf("bucket %d s: %v", secs, err)
		}
		if n := BucketCount(24*time.Hour, req.GetBucket().AsDuration()); n != 1 {
			t.Errorf("bucket %d s: %d buckets in a day, want 1", secs, n)
		}
	}
	if n := BucketCount(24*time.Hour, 7*time.Hour); n != 4 {
		t.Errorf("24 h in 7 h buckets = %d, want 4", n)
	}
	if n := BucketCount(24*time.Hour, 6*time.Hour); n != 4 {
		t.Errorf("24 h in 6 h buckets = %d, want 4", n)
	}
}
