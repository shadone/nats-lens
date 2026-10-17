package fake

import (
	"context"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// SystemSummary is empty: the handler adds the system reference.
func (p *Provider) SystemSummary(context.Context, *v1.ListSystemsRequest) (*v1.SystemSummary, error) {
	return &v1.SystemSummary{}, nil
}

// ListAccounts returns the scenario's one account with its connections,
// streams and consumers.
func (p *Provider) ListAccounts(context.Context, *v1.ListAccountsRequest) (*v1.ListAccountsResponse, error) {
	var conns int32
	for _, sp := range p.sc.servers {
		if sp.answering() {
			conns += int32(sp.conns)
		}
	}
	var consumers int32
	for _, sp := range p.sc.streams {
		consumers += int32(len(sp.consumers) + sp.ephemeral)
	}
	return &v1.ListAccountsResponse{Accounts: []*v1.Account{{
		Account: Account, Connections: ptr(conns), Streams: ptr(int32(len(p.sc.streams))), Consumers: ptr(consumers),
	}}}, nil
}
