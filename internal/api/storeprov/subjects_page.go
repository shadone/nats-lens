package storeprov

import (
	"context"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// Subjects reads nothing yet: the subscription tables come with the
// collection of subscription lists, and the store part is built on them.
func (p *Provider) Subjects(_ context.Context, _ *v1.GetSubjectsRequest) (*api.SubjectsFacts, error) {
	return &api.SubjectsFacts{}, nil
}

// MatchSubject reads nothing yet, as Subjects.
func (p *Provider) MatchSubject(_ context.Context, _ *v1.MatchSubjectRequest) (*api.MatchFacts, error) {
	return &api.MatchFacts{}, nil
}
