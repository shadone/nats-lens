package api_test

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func storageBytes(logical, observed, estimate int64) *v1.StorageChangeBytes {
	return &v1.StorageChangeBytes{Logical: proto.Int64(logical), ObservedReplicas: proto.Int64(observed), EstimatedReplicas: proto.Int64(estimate)}
}
func storageFacts() (*v1.GetStorageChangeRequest, *api.StorageChangeFacts) {
	a := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	b := a.Add(time.Hour)
	return &v1.GetStorageChangeRequest{System: "example-prod", A: timestamppb.New(a), B: timestamppb.New(b), PageSize: 100}, &api.StorageChangeFacts{
		A: &v1.StorageChangeEndpoint{Epoch: 1, Requested: timestamppb.New(a), StartedAt: timestamppb.New(a), FinishedAt: timestamppb.New(a), InventoryComplete: true},
		B: &v1.StorageChangeEndpoint{Epoch: 2, Requested: timestamppb.New(b), StartedAt: timestamppb.New(b), FinishedAt: timestamppb.New(b), InventoryComplete: true},
		Rows: []*v1.StorageChangeRow{
			{Id: "orders", RowKind: "resource", Account: "APP", Stream: "orders", Storage: "file", Before: storageBytes(100, 300, 300), After: storageBytes(130, 390, 390)},
			{Id: "archive", RowKind: "resource", Account: "APP", Stream: "archive", Storage: "file", Before: storageBytes(50, 150, 150), After: storageBytes(30, 90, 90)},
			{Id: "memory", RowKind: "resource", Account: "APP", Stream: "sessions", Storage: "memory", Before: storageBytes(10000, 10000, 10000), After: storageBytes(11000, 11000, 11000)},
			{Id: "server", RowKind: "server", Server: "nats-0", Storage: "file", Before: &v1.StorageChangeBytes{ServerReported: proto.Int64(500)}, After: &v1.StorageChangeBytes{ServerReported: proto.Int64(550)}},
		}}
}
func storageTotal(t *testing.T, r *v1.GetStorageChangeResponse, storage string) *v1.StorageChangeTotals {
	t.Helper()
	for _, x := range r.Totals {
		if x.Storage == storage {
			return x
		}
	}
	t.Fatalf("missing %s total", storage)
	return nil
}
func TestStorageChangeSignedContributions(t *testing.T) {
	req, f := storageFacts()
	before := proto.Clone(f.Rows[0]).(*v1.StorageChangeRow)
	res, err := api.FinishStorageChange(req, f)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 4 {
		t.Fatalf("rows=%d; want all endpoint resources and stores", len(res.Rows))
	}
	total := storageTotal(t, res, "file")
	if total.GetDelta().GetLogical() != 10 || total.GetDelta().GetObservedReplicas() != 30 || total.GetDelta().GetServerReported() != 50 || total.Unreconciled == nil || *total.Unreconciled != 20 {
		t.Fatalf("wrong separate quantities: %v", total)
	}
	if res.Rows[0].Id != "memory" || res.Rows[1].Id != "orders" || res.Rows[2].Delta.GetLogical() != -20 {
		t.Fatalf("signed ranking=%v", res.Rows)
	}
	if !proto.Equal(before, f.Rows[0]) {
		t.Fatal("finisher mutated input evidence")
	}
}
func TestStorageChangeMissingNeverZero(t *testing.T) {
	req, f := storageFacts()
	f.Rows[0].After = nil
	f.B.InventoryComplete = false
	res, err := api.FinishStorageChange(req, f)
	if err != nil {
		t.Fatal(err)
	}
	total := storageTotal(t, res, "file")
	if total.Delta.Logical != nil || total.Unreconciled != nil || total.GetUnknownRows() != 1 || total.GetKnownContributions().GetLogical() != -20 {
		t.Fatalf("missing became zero/complete: %v", total)
	}
	for _, r := range res.Rows {
		if r.Id == "orders" && r.GetDelta().Logical != nil {
			t.Fatal("missing contribution fabricated")
		}
	}
}
func TestStorageChangeAccountResidualUnavailable(t *testing.T) {
	req, f := storageFacts()
	req.Account = "APP"
	res, err := api.FinishStorageChange(req, f)
	if err != nil {
		t.Fatal(err)
	}
	if storageTotal(t, res, "file").Unreconciled != nil {
		t.Fatal("account resources reconciled to system-wide server usage")
	}
	found := false
	for _, r := range res.Reasons {
		found = found || r == "server_usage_is_system_wide"
	}
	if !found {
		t.Fatal("system-wide usage not disclosed")
	}
}
func TestStorageChangePaginationAndPins(t *testing.T) {
	req, f := storageFacts()
	req.PageSize = 1
	first, err := api.FinishStorageChange(req, f)
	if err != nil {
		t.Fatal(err)
	}
	if first.TotalCount != 4 || len(first.Rows) != 1 || first.NextPageToken == "" || first.GetA().GetEpoch() != 1 || first.GetB().GetEpoch() != 2 {
		t.Fatalf("bounded page/pins=%v", first)
	}
	seen := map[string]bool{first.Rows[0].Id: true}
	req.PageToken = first.NextPageToken
	for req.PageToken != "" {
		r, err := api.FinishStorageChange(req, f)
		if err != nil {
			t.Fatal(err)
		}
		if seen[r.Rows[0].Id] {
			t.Fatal("duplicate page row")
		}
		seen[r.Rows[0].Id] = true
		req.PageToken = r.NextPageToken
	}
	if len(seen) != 4 {
		t.Fatal("lost rows")
	}
	req.PageToken = first.NextPageToken
	req.Account = "OTHER"
	if _, err := api.FinishStorageChange(req, f); !errors.Is(err, api.ErrInvalid) {
		t.Fatalf("wrong scope token: %v", err)
	}
	req.Account = ""
	f.Rows[0].After.Logical = proto.Int64(131)
	if _, err := api.FinishStorageChange(req, f); !errors.Is(err, api.ErrFailedPrecondition) {
		t.Fatalf("changed evidence token: %v", err)
	}
}
func TestStorageChangeOverflowAndNegativeMeasurements(t *testing.T) {
	for _, value := range []int64{math.MaxInt64, -1} {
		t.Run(string(rune(value&127)), func(t *testing.T) {
			req, f := storageFacts()
			f.Rows[0].Before.Logical = proto.Int64(value)
			if _, err := api.FinishStorageChange(req, f); err == nil {
				t.Fatal("invalid/overflow bytes accepted")
			}
		})
	}
}
