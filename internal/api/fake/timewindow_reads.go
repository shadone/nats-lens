package fake

import (
	"context"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/proto"
)

func (p *Provider) GetTimeWindows(ctx context.Context, req *v1.GetTimeWindowsRequest) (*v1.GetTimeWindowsResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &v1.GetTimeWindowsResponse{Range: proto.Clone(req.Range).(*v1.TimeRange), Windows: p.windows(p.scen(), req.Range)}, nil
}
