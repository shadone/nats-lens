package fake

import (
	"context"
	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"testing"
	"time"
)

func TestStorageChangeDemoNoInventedMemoryUsage(t *testing.T) {
	p := New(false)
	req := &v1.GetStorageChangeRequest{A: ts(FixedNow.Add(-time.Hour)), B: ts(FixedNow), PageSize: 100}
	f, err := p.StorageChange(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	res, err := api.FinishStorageChange(req, f)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range res.Totals {
		if v.Storage == "memory" && v.GetAfter().ServerReported != nil {
			t.Fatal("Capacity has no memory-store measurements; demo invented zero usage")
		}
	}
}
func TestStorageChangeDemoAlignedPinReplay(t *testing.T) {
	p := New(true)
	p.clock = func() time.Time { return FixedNow.Add(7 * time.Second) }
	req := &v1.GetStorageChangeRequest{A: ts(FixedNow.Add(-time.Hour)), B: ts(FixedNow)}
	f, err := p.StorageChange(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	req.EpochA = f.A.Epoch
	req.EpochB = f.B.Epoch
	replay, err := p.StorageChange(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !replay.A.StartedAt.AsTime().Equal(f.A.StartedAt.AsTime()) || !replay.B.StartedAt.AsTime().Equal(f.B.StartedAt.AsTime()) {
		t.Fatal("demo pin moved its observations")
	}
}
func TestStorageChangeDemoUsesOneClock(t *testing.T) {
	p := New(true)
	calls := 0
	p.clock = func() time.Time { calls++; return FixedNow.Add(time.Duration(calls) * time.Second) }
	_, err := p.StorageChange(context.Background(), &v1.GetStorageChangeRequest{A: ts(FixedNow.Add(-time.Hour)), B: ts(FixedNow)})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("comparison reread moving scenario clock %d times", calls)
	}
}
