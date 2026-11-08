package api

import (
	"connectrpc.com/connect"
	"context"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"time"
)

const SlowConsumerAttributionLimit = "Counters identify a server process and collection interval, not an exact detection time or connection. Recorded disconnects and findings are separate evidence in the selected range; their counts need not match detections. A detection does not establish application slowness, message loss or a disconnect."

func (h *Handler) GetSlowConsumerInvestigation(ctx context.Context, req *connect.Request[v1.GetSlowConsumerInvestigationRequest]) (*connect.Response[v1.GetSlowConsumerInvestigationResponse], error) {
	return call(ctx, h, "GetSlowConsumerInvestigation", req.Msg, func(p Provider, m *v1.GetSlowConsumerInvestigationRequest) error {
		// ASVS 2.2.1: bounds are enforced by the service, including agent callers.
		if err := withRange(p, &m.Range); err != nil {
			return err
		}
		if m.Range.End.AsTime().Sub(m.Range.Start.AsTime()) > 30*24*time.Hour {
			return invalid("investigation range exceeds 30 days")
		}
		if (m.ObservationServerId == "") != (m.ObservationEpoch == 0) || m.ObservationEpoch < 0 {
			return invalid("counter selection requires a process ID and positive collection epoch")
		}
		if len(m.Server) > MaxNameLength || len(m.Account) > MaxNameLength || len(m.ObservationServerId) > MaxNameLength {
			return invalid("server or account too long")
		}
		var err error
		m.Limit, err = pageSize(m.Limit, 200)
		return err
	}, on(Provider.GetSlowConsumerInvestigation))
}
