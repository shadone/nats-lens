package storeprov

import (
	"context"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/proto"
)

// GetTimeWindows reuses the bounded system-wide derivation without Overview's
// findings, server summaries or tile reads. Membership bounds do not freeze
// updates/deletions or establish complete event capture.
func (p *Provider) GetTimeWindows(ctx context.Context, req *v1.GetTimeWindowsRequest) (*v1.GetTimeWindowsResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	windows, err := p.k8sWindows(ctx, req.GetRange())
	if err != nil {
		return nil, err
	}
	return &v1.GetTimeWindowsResponse{Range: proto.Clone(req.Range).(*v1.TimeRange), Windows: windows}, nil
}
