package timing

import (
	"context"
	"sync/atomic"
	"time"
)

// QueryInfo is one finished query as a query hook receives it.
type QueryInfo struct {
	System, Source string
	// Procedure is the API call that ran the query, "" outside one.
	Procedure string
	SQL       string
	Args      []any
	Took      time.Duration
}

var queryHook atomic.Pointer[func(QueryInfo)]

// SetQueryHook makes every query of every store call f with its text and
// arguments; nil removes it. natslens never sets one: the text and the
// arguments name entities and values. natslens-perf sets one to find the
// heaviest queries and replay them under EXPLAIN ANALYZE.
func SetQueryHook(f func(QueryInfo)) {
	if f == nil {
		queryHook.Store(nil)
		return
	}
	queryHook.Store(&f)
}

// HookSet reports whether a query hook is set, so that a caller builds
// the hook's arguments only then.
func HookSet() bool { return queryHook.Load() != nil }

// Hook passes a finished query to the hook, if one is set.
func Hook(ctx context.Context, system, sql string, args []any, took time.Duration) {
	f := queryHook.Load()
	if f == nil {
		return
	}
	info := QueryInfo{System: system, Source: Source(ctx), SQL: sql, Args: args, Took: took}
	if c, ok := ctx.Value(callKey{}).(*Call); ok {
		info.Procedure = c.Procedure
	}
	(*f)(info)
}
