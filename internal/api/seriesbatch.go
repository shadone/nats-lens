package api

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	"connectrpc.com/connect"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// MaxBatchQueries is the number of series one GetTimeSeriesBatch call may
// ask for.
const MaxBatchQueries = 32

// batchParallelism is the number of series of one batch read at a time.
const batchParallelism = 4

// BatchTimeout bounds the time one GetTimeSeriesBatch call may spend in
// the provider; each of its queries has QueryTimeout of its own within it.
// It stays under the web client's 45-second call timeout, which the
// client sends as Connect-Timeout-Ms and which bounds every call.
const BatchTimeout = 40 * time.Second

// The limits of a batch and of each of its queries; tests shorten them.
var (
	batchTimeout      = BatchTimeout
	batchQueryTimeout = QueryTimeout
)

// GetTimeSeriesBatch reads several series over one range. The range is
// resolved once, so every series ends at the same instant. The call has
// BatchTimeout, each query QueryTimeout within it. A query that
// fails as GetTimeSeries would with invalid_argument or not_found gets its
// error in its result; any other failure fails the call.
func (h *Handler) GetTimeSeriesBatch(ctx context.Context, req *connect.Request[v1.GetTimeSeriesBatchRequest]) (*connect.Response[v1.GetTimeSeriesBatchResponse], error) {
	return callWithin(ctx, h, "GetTimeSeriesBatch", batchTimeout, req.Msg, func(p Provider, m *v1.GetTimeSeriesBatchRequest) error {
		return normalizeTimeSeriesBatch(m, p)
	}, func(s System, ctx context.Context, m *v1.GetTimeSeriesBatchRequest) (*v1.GetTimeSeriesBatchResponse, error) {
		return readBatch(ctx, s.Provider, m)
	})
}

// normalizeTimeSeriesBatch checks the number of queries and resolves the
// range and the default max_points; the queries are checked one by one in
// readBatch, so that a bad one fails alone.
func normalizeTimeSeriesBatch(req *v1.GetTimeSeriesBatchRequest, p Provider) error {
	switch n := len(req.GetQueries()); {
	case n == 0:
		return invalid("queries is empty")
	case n > MaxBatchQueries:
		return invalid("queries has %d entries, at most %d", n, MaxBatchQueries)
	}
	r, err := ResolveRange(req.GetRange(), rangeReference(p), DefaultRange)
	if err != nil {
		return err
	}
	req.Range = r
	req.MaxPoints, err = points(req.GetMaxPoints(), DefaultSeriesPoints)
	return err
}

// readBatch reads each query of a normalized request, a few at a time.
func readBatch(ctx context.Context, p Provider, req *v1.GetTimeSeriesBatchRequest) (*v1.GetTimeSeriesBatchResponse, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make([]*v1.SeriesResult, len(req.GetQueries()))
	var (
		wg       sync.WaitGroup
		once     sync.Once
		firstErr error
	)
	sem := make(chan struct{}, batchParallelism)
	for i, q := range req.GetQueries() {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			res, err := readQueryRecovered(ctx, p, req, q)
			if err != nil {
				once.Do(func() { firstErr = err; cancel() })
				return
			}
			results[i] = res
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &v1.GetTimeSeriesBatchResponse{Results: results, Range: req.GetRange()}, nil
}

// readQueryRecovered is readQuery with a panic turned into an error that
// fails the call: the worker runs outside the handler's goroutine, where
// the server's recovery does not reach.
func readQueryRecovered(ctx context.Context, p Provider, batch *v1.GetTimeSeriesBatchRequest, q *v1.SeriesQuery) (res *v1.SeriesResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			res, err = nil, fmt.Errorf("series %s panicked: %v\n%s", q.GetMetric(), r, debug.Stack())
		}
	}()
	return readQuery(ctx, p, batch, q)
}

// readQuery reads one query as GetTimeSeries would, with the batch's
// resolved range. An invalid or unknown metric or entity is the result's
// error; any other error is returned.
func readQuery(ctx context.Context, p Provider, batch *v1.GetTimeSeriesBatchRequest, q *v1.SeriesQuery) (*v1.SeriesResult, error) {
	sub := &v1.GetTimeSeriesRequest{
		Metric:    q.GetMetric(),
		Entity:    q.GetEntity(),
		Range:     batch.GetRange(),
		MaxPoints: q.GetMaxPoints(), IncludeObservations: q.GetIncludeObservations(),
		System: batch.GetSystem(),
	}
	if sub.MaxPoints == 0 {
		sub.MaxPoints = batch.GetMaxPoints()
	}
	err := normalizeTimeSeries(sub, p.Now())
	var res *v1.GetTimeSeriesResponse
	if err == nil {
		qctx, cancel := context.WithTimeout(ctx, batchQueryTimeout)
		res, err = p.GetTimeSeries(qctx, sub)
		cancel()
	}
	switch {
	case err == nil:
		return &v1.SeriesResult{Series: res.GetSeries()}, nil
	case errors.Is(err, ErrInvalid), errors.Is(err, ErrNotFound):
		return &v1.SeriesResult{Error: err.Error()}, nil
	}
	return nil, err
}
