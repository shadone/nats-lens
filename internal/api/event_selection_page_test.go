package api

import (
	"context"
	"fmt"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"testing"
)

type selectionPageSpy struct {
	calls   int
	request *v1.GetEventBatchRequest
}

func (s *selectionPageSpy) GetEventBatch(_ context.Context, r *v1.GetEventBatchRequest) (*v1.GetEventBatchResponse, error) {
	s.calls++
	s.request = r
	rows := make([]*v1.Event, len(r.Ids))
	for i, id := range r.Ids {
		rows[i] = &v1.Event{Id: id}
	}
	return &v1.GetEventBatchResponse{Events: rows, SelectedIds: r.Ids, EventReadId: r.EventReadId, TotalCount: int32(len(rows))}, nil
}
func TestSelectionPageReadsOnlyOneBoundedBatch(t *testing.T) {
	ids := make([]string, 2000)
	for i := range ids {
		ids[i] = fmt.Sprint(i + 1)
	}
	boundary := int64(2000)
	spy := new(selectionPageSpy)
	r, err := ReadEventSelection(context.Background(), spy, &v1.ListEventSelectionRequest{Ids: ids, Offset: 100, EventReadId: &boundary, System: "shop"})
	if err != nil || spy.calls != 1 || len(spy.request.Ids) != 50 || spy.request.Ids[0] != "101" || spy.request.System != "shop" || spy.request.GetEventReadId() != boundary {
		t.Fatalf("unbounded/unscoped read: %+v %v", spy, err)
	}
	if r.SelectedTotalCount != 2000 || len(r.SelectedIds) != 2000 || r.Offset != 100 || r.NextOffset != 150 || !FitsEntityPage(r) {
		t.Fatalf("membership lost: %+v", r)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadEventSelection(ctx, spy, &v1.ListEventSelectionRequest{Ids: ids}); err == nil || spy.calls != 1 {
		t.Fatal("cancelled selection performed a read")
	}
	if _, err := ReadEventSelection(context.Background(), spy, &v1.ListEventSelectionRequest{}); err == nil || spy.calls != 1 {
		t.Fatal("invalid selection performed a read")
	}
}
