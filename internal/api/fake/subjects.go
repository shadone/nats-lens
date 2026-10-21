package fake

import (
	"context"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// Subjects reads the scenario's streams and subscriptions.
func (p *Provider) Subjects(_ context.Context, req *v1.GetSubjectsRequest) (*api.SubjectsFacts, error) {
	return &api.SubjectsFacts{}, nil
}

// MatchSubject reads what Subjects reads for the tester.
func (p *Provider) MatchSubject(_ context.Context, req *v1.MatchSubjectRequest) (*api.MatchFacts, error) {
	return &api.MatchFacts{}, nil
}
