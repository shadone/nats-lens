package storeprov

import (
	"context"
	"fmt"
	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// GetEventBatch reads exact retained identities, including window inputs outside
// the original Timeline range. It never infers membership from timestamps.
func (p *Provider) GetEventBatch(ctx context.Context, req *v1.GetEventBatchRequest) (*v1.GetEventBatchResponse, error) {
	if err := api.EventBatchSelection(req.Ids, req.EventReadId); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	query := &v1.ListEventsRequest{SelectedIds: req.Ids, EventReadId: req.EventReadId, PageSize: int32(len(req.Ids))}
	rows, err := p.listEvents(ctx, query)
	if err != nil {
		return nil, err
	}
	res := &v1.GetEventBatchResponse{Events: rows.Events, TotalCount: rows.TotalCount, SelectedIds: append([]string(nil), req.Ids...), EventReadId: req.EventReadId}
	if !api.FitsEntityPage(res) {
		return nil, fmt.Errorf("%w: selected event response exceeds page budget", api.ErrResourceExhausted)
	}
	return res, nil
}
