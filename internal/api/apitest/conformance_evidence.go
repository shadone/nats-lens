package apitest

import (
	"fmt"
	"slices"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// scopedFindings reads the firing findings, muted ones included, that
// ListFindings keeps for account.
func (q Q) scopedFindings(account string) *v1.ListFindingsResponse {
	q.t.Helper()
	return must(q.env.Client.ListFindings(ctx, connect.NewRequest(&v1.ListFindingsRequest{System: q.System, Range: q.day(),
		IncludeMuted: true, PageSize: 500, Account: account}))).of(q.t)
}

// accountScope checks ListFindings' account filter against the unscoped
// list: for each account a finding names, and for one no finding names,
// it keeps that account's findings and those of no account, in the same
// order, and counts the others. It reports the accounts it checked, or ""
// without both an account's finding and one of no account.
func accountScope(q Q) string {
	all := q.scopedFindings("").GetFindings()
	var accounts []string
	unbound := 0
	for _, f := range all {
		if a := api.FindingAccount(f.GetEntity()); a == "" {
			unbound++
		} else if !slices.Contains(accounts, a) {
			accounts = append(accounts, a)
		}
	}
	if len(accounts) == 0 || unbound == 0 {
		return ""
	}
	for _, account := range append(slices.Clone(accounts), "NO-SUCH-ACCOUNT") {
		var want []string
		other := 0
		for _, f := range all {
			if a := api.FindingAccount(f.GetEntity()); a == "" || a == account {
				want = append(want, f.GetId())
			} else {
				other++
			}
		}
		res := q.scopedFindings(account)
		var got []string
		for _, f := range res.GetFindings() {
			got = append(got, f.GetId())
		}
		if !slices.Equal(got, want) || res.GetOtherAccountCount() != int32(other) || res.GetTotalCount() != int32(len(want)) {
			q.t.Errorf("account %s: findings %v (total %d), %d of other accounts; want %v, %d", account, got, res.GetTotalCount(),
				res.GetOtherAccountCount(), want, other)
			return ""
		}
	}
	return fmt.Sprintf("%d accounts, %d findings of no account", len(accounts), unbound)
}

// assetPlacementAgrees checks that ListAssetPlacement places every
// stream and durable consumer of the matrix as its cells do: the same
// leader and the same servers in the same states, in order. It reports how
// many assets it compared, or "" without one.
func assetPlacementAgrees(q Q) string {
	matrix := assets(q.placement(v1.PlacementFilter_PLACEMENT_FILTER_UNSPECIFIED))
	compared := 0
	for chunk := range slices.Chunk(matrix, api.MaxAssetPlacement) {
		req := &v1.ListAssetPlacementRequest{System: q.System, Range: q.day()}
		for _, a := range chunk {
			req.Entities = append(req.Entities, &v1.EntityRef{Type: a.GetEntity().GetType(), Key: a.GetEntity().GetKey()})
		}
		res := must(q.env.Client.ListAssetPlacement(ctx, connect.NewRequest(req))).of(q.t)
		if len(res.GetAssets()) != len(chunk) {
			q.t.Errorf("%d assets asked, %d answered", len(chunk), len(res.GetAssets()))
			return ""
		}
		for i, a := range chunk {
			got := res.GetAssets()[i]
			var want, have []string
			for _, c := range a.GetCells() {
				if c.GetServer() == "" {
					continue
				}
				want = append(want, c.GetServer()+" "+c.GetState().String())
			}
			for _, r := range got.GetReplicas() {
				have = append(have, r.GetServer()+" "+r.GetState().String())
			}
			if got.GetEntity().GetKey() != a.GetEntity().GetKey() || got.GetLeader() != a.GetLeader() || !slices.Equal(have, want) ||
				got.GetReported() != (len(want) > 0) {
				q.t.Errorf("%s: placement %v led by %q (reported %v); the matrix has %v led by %q", a.GetEntity().GetKey(), have,
					got.GetLeader(), got.GetReported(), want, a.GetLeader())
				return ""
			}
			if len(want) > 0 {
				compared++
			}
		}
	}
	if compared == 0 {
		return ""
	}
	return fmt.Sprintf("%d assets", compared)
}

// evidenceRows are the states behind the Overview's cause cards: the
// account scope of the findings and the placement of their assets.
func evidenceRows() []State {
	return []State{
		{"findings of one account, with the findings of no account and a count of the others", accountScope},
		{"the placement of a few assets, cell for cell as the matrix places them", everySystem(assetPlacementAgrees)},
	}
}
