package api_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// Decode public protobuf JSON so the missing additive contract reaches runtime RED.
func comparisonOptions(t *testing.T, req *v1.GetStorageChangeRequest, options map[string]any) {
	t.Helper()
	raw, err := protojson.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err = json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for k, v := range options {
		fields[k] = v
	}
	raw, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err = protojson.Unmarshal(raw, req); err != nil {
		t.Fatal(err)
	}
}
func TestStorageChangeGlobalSignedOrdering(t *testing.T) {
	for _, tc := range []struct {
		name  string
		order int
		ids   []string
	}{{"increases", 2, []string{"memory", "orders", "archive", "unknown"}}, {"decreases", 1, []string{"archive", "orders", "memory", "unknown"}}} {
		t.Run(tc.name, func(t *testing.T) {
			req, f := storageFacts()
			req.PageSize = 1
			f.Rows = append(f.Rows, &v1.StorageChangeRow{Id: "unknown", RowKind: "resource", Account: "APP", Stream: "unreported", Storage: "file"})
			comparisonOptions(t, req, map[string]any{"order": tc.order, "resourcesOnly": true})
			var all []string
			for {
				res, err := api.FinishStorageChange(req, f)
				if err != nil {
					t.Fatal(err)
				}
				if res.TotalCount != 4 {
					t.Fatalf("expected four resource matches, got %d", res.TotalCount)
				}
				for _, r := range res.Rows {
					all = append(all, r.Id)
				}
				if res.NextPageToken == "" {
					break
				}
				req.PageToken = res.NextPageToken
			}
			if len(all) != len(tc.ids) {
				t.Fatal(all)
			}
			for i, id := range all {
				if id != tc.ids[i] {
					t.Fatalf("wanted %v got %v", tc.ids, all)
				}
			}
		})
	}
}
func TestStorageChangeTypeFilterPreservesGlobalTotals(t *testing.T) {
	req, f := storageFacts()
	f.Rows[0].Kind = v1.CapacityStreamKind_CAPACITY_STREAM_KIND_STREAM
	f.Rows[1].Kind = v1.CapacityStreamKind_CAPACITY_STREAM_KIND_OBJECT_STORE
	f.Rows[2].Kind = v1.CapacityStreamKind_CAPACITY_STREAM_KIND_KEY_VALUE
	all, err := api.FinishStorageChange(req, f)
	if err != nil {
		t.Fatal(err)
	}
	comparisonOptions(t, req, map[string]any{"resourceKind": 2, "resourcesOnly": true})
	res, err := api.FinishStorageChange(req, f)
	if err != nil {
		t.Fatal(err)
	}
	if res.TotalCount != 1 || len(res.Rows) != 1 || res.Rows[0].Id != "memory" {
		t.Fatal(res)
	}
	for i, total := range res.Totals {
		if !proto.Equal(total.Before, all.Totals[i].Before) || !proto.Equal(total.After, all.Totals[i].After) || !proto.Equal(total.Delta, all.Totals[i].Delta) {
			t.Fatal("row filter changed all-type totals")
		}
	}
	if got := storageTotal(t, res, "file").GetOmittedContributions().GetLogical(); got != 10 {
		t.Fatalf("filtered-out known file contributions lost: %d", got)
	}
}
func TestStorageChangeOrderAndFilterBindContinuation(t *testing.T) {
	for _, key := range []string{"order", "resourceKind", "resourcesOnly"} {
		t.Run(key, func(t *testing.T) {
			req, f := storageFacts()
			req.PageSize = 1
			res, err := api.FinishStorageChange(req, f)
			if err != nil {
				t.Fatal(err)
			}
			req.PageToken = res.NextPageToken
			value := any(1)
			if key == "resourcesOnly" {
				value = true
			}
			comparisonOptions(t, req, map[string]any{key: value})
			if _, err = api.FinishStorageChange(req, f); !errors.Is(err, api.ErrInvalid) {
				t.Fatalf("changed %s accepted continuation: %v", key, err)
			}
		})
	}
}
func TestStorageChangeRejectsUnknownOrderAndKind(t *testing.T) {
	for _, field := range []string{"order", "resourceKind"} {
		req, f := storageFacts()
		comparisonOptions(t, req, map[string]any{field: 999})
		if _, err := api.FinishStorageChange(req, f); !errors.Is(err, api.ErrInvalid) {
			t.Fatalf("unknown %s accepted: %v", field, err)
		}
	}
}
