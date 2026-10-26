package fake

import (
	"context"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// SystemSummary summarizes the scenario as storeprov does from collected
// data: the newest epoch's counts, the findings at the range's end, the
// strip from the timeline's rows, and the scenario's collection state. The
// handler adds the system reference.
func (p *Provider) SystemSummary(_ context.Context, req *v1.ListSystemsRequest) (*v1.SystemSummary, error) {
	s := p.scen()
	r := req.GetRange()
	all := p.findings(s)
	cur := current(all, r)
	res := &v1.SystemSummary{
		Collection: p.sc.collection, CollectionError: p.sc.collectionError,
		Freshness: s.freshness(r), Status: v1.Status_STATUS_NO_DATA, WorstFinding: worstUnmuted(cur), Tally: tally(all, r),
	}
	if !p.sc.errorSince.IsZero() {
		res.CollectionErrorSince = ts(p.sc.errorSince.Add(s.shift))
	}
	if at, _, ok := s.epochBefore(r); ok {
		res.Status, _ = worstStatus(cur, nil)
		// Answering servers and connections as of the range's newest
		// epoch, as freshness's partial.
		var answering, conns int64
		clusters := map[string]bool{}
		for _, sp := range p.sc.servers {
			if !s.downAt(sp, at) {
				answering++
				conns += p.sc.connsOn(sp, s.clock(at))
			}
			if sp.cluster != "" {
				clusters[sp.cluster] = true
			}
		}
		res.Servers, res.ServersAnswering = ptr(int32(len(p.sc.servers))), ptr(int32(answering))
		res.Clusters, res.Streams, res.Connections = ptr(int32(len(clusters))), ptr(int32(len(p.sc.streams))), ptr(conns)
	}
	bucket := api.StripBucket(r, req.GetStripBuckets())
	start, end := r.GetStart().AsTime(), r.GetEnd().AsTime()
	res.StripStart, res.StripBucket = r.GetStart(), dur(bucket)
	res.Strip = make([]v1.Status, api.BucketCount(end.Sub(start), bucket))
	rows := s.timelineRows(r, bucket, nil, true)
	for i := range res.Strip {
		res.Strip[i] = v1.Status_STATUS_NO_DATA
		for _, row := range rows {
			if c := row.GetCells()[i]; statusRank(c) < statusRank(res.Strip[i]) {
				res.Strip[i] = c
			}
		}
	}
	return res, nil
}

// ListAccounts returns the scenario's accounts in identifier order: the
// stream account with its connections, streams and consumers, and the
// others of its Accounts and auth page.
func (p *Provider) ListAccounts(_ context.Context, req *v1.ListAccountsRequest) (*v1.ListAccountsResponse, error) {
	s := p.scen()
	at, _, ok := s.epochBefore(req.GetRange())
	res := &v1.ListAccountsResponse{}
	for _, a := range s.accounts(at) {
		acc := &v1.Account{Account: a.Account, AccountName: a.Name, IsSystem: a.IsSystem}
		// Without an epoch in the range the counts are unknown.
		if ok {
			acc.Connections, acc.Streams, acc.Consumers = a.Connections, a.Streams, ptr(int32(0))
			if a.Account == Account {
				var consumers int32
				for _, sp := range p.sc.streams {
					consumers += int32(len(sp.consumers) + sp.ephemeral)
				}
				acc.Consumers = ptr(consumers)
			}
		}
		res.Accounts = append(res.Accounts, acc)
	}
	return res, nil
}
