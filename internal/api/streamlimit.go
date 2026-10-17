package api

import v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"

// LimitStreams sets the response's total count of matching streams and
// keeps at most limit of them, in their order; zero or a negative limit
// keeps all. The totals are left as they are, across every match.
func LimitStreams(res *v1.ListStreamsResponse, limit int32) *v1.ListStreamsResponse {
	res.TotalCount = int32(len(res.GetStreams()))
	if limit > 0 && int(limit) < len(res.GetStreams()) {
		res.Streams = res.GetStreams()[:limit]
	}
	return res
}
