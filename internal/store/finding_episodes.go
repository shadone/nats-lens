package store

import (
	"context"
	"time"
)

// FindingEpisodeQuery selects retained occurrences overlapping [From, To).
// FindingID and StartEpoch select a known occurrence without substituting a
// later recurrence. Limits are validated independently of public API callers.
type FindingEpisodeQuery struct {
	From, To                         time.Time
	FindingID                        string
	StartEpoch                       int64
	CheckCode, EntityType, EntityKey string
	Limit, Offset                    int
}

// FindingEpisode preserves the observed start identity independently of the
// current findings row. Unknown starts are retained fragments, not invented onsets.
type FindingEpisode struct {
	Finding            Finding
	StartKnown         bool
	FirstRetainedEpoch int64
}

type FindingEpisodePage struct {
	Items []FindingEpisode
	Total int
}

// FindingEpisodes is a separate projection: FindingsAt intentionally retains
// its established newest-episode-per-finding contract.
func (s *Store) FindingEpisodes(ctx context.Context, q FindingEpisodeQuery) (FindingEpisodePage, error) {
	// Runtime RED stub; implementation follows the occurrence regressions.
	return FindingEpisodePage{}, nil
}
