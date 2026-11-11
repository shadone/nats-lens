package storeprov

import (
	"github.com/shadone/nats-lens/internal/admit"
	"github.com/shadone/nats-lens/internal/api"
)

var _ api.QueryBudgeter = (*Provider)(nil)

// QueryBudget returns the store's query budget, nil when it has none.
func (p *Provider) QueryBudget() *admit.Gate { return p.St.QueryBudget() }
