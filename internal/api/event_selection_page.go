package api

import (
	"context"
	"fmt"
	"github.com/shadone/nats-lens/internal/eventselection"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

type eventBatchReader interface {
	GetEventBatch(context.Context, *v1.GetEventBatchRequest) (*v1.GetEventBatchResponse, error)
}

func NormalizeEventSelection(r *v1.ListEventSelectionRequest) error {
	p, err := eventselection.PageFor(r.Ids, r.EventReadId, r.Offset, r.PageSize)
	if err != nil {
		return invalid("%s", err)
	}
	r.PageSize = p.Size
	return nil
}

// ReadEventSelection keeps the source reader authoritative. Only one selected
// page reaches its parameterized identity lookup; no ranking/all-history scan
// or payload RPC is added. Captured membership is not an authorization token.
func ReadEventSelection(ctx context.Context, reader eventBatchReader, r *v1.ListEventSelectionRequest) (*v1.ListEventSelectionResponse, error) {
	if err := NormalizeEventSelection(r); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	page, _ := eventselection.PageFor(r.Ids, r.EventReadId, r.Offset, r.PageSize)
	batch, err := reader.GetEventBatch(ctx, &v1.GetEventBatchRequest{Ids: page.IDs, EventReadId: r.EventReadId, System: r.System})
	if err != nil {
		return nil, err
	}
	res := &v1.ListEventSelectionResponse{Events: batch.Events, SelectedIds: append([]string(nil), r.Ids...), EventReadId: batch.EventReadId, TotalCount: batch.TotalCount, SelectedTotalCount: page.Total, Offset: page.Offset, NextOffset: page.Next, PageSize: page.Size}
	if !FitsEntityPage(res) {
		return nil, fmt.Errorf("%w: event selection response exceeds page budget; reduce page size", ErrResourceExhausted)
	}
	return res, nil
}
