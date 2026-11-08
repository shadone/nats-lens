package api

import v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"

// LimitStreams sets the response's total count of matching streams and
// keeps at most limit of them, in their order. Zero uses 100 and larger
// limits are capped at 100. Totals remain across every match.
// Providers use EntityPages to expose continuations and reject negative limits.
func LimitStreams(res *v1.ListStreamsResponse, limit int32) *v1.ListStreamsResponse {
	res.TotalCount = int32(len(res.GetStreams()))
	if limit <= 0 || limit > MaxEntityPage {
		limit = MaxEntityPage
	}
	if int(limit) < len(res.GetStreams()) {
		res.Streams = res.GetStreams()[:limit]
	}
	return res
}
