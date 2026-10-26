package apitest

import (
	"fmt"
	"slices"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// apps reads the Applications page of a range in an account scope.
func (q Q) apps(r *v1.TimeRange, account string, tooling bool) *v1.GetApplicationsResponse {
	q.t.Helper()
	return must(q.env.Client.GetApplications(ctx, connect.NewRequest(&v1.GetApplicationsRequest{System: q.System, Range: r,
		Account: account, IncludeTooling: tooling}))).of(q.t)
}

// application finds an application of the day's page that ok accepts.
func application(ok func(*v1.GetApplicationsResponse, *v1.Application) bool) func(Q) string {
	return func(q Q) string {
		res := q.apps(q.day(), "", false)
		for _, a := range res.GetApplications() {
			if ok(res, a) {
				return a.GetName()
			}
		}
		return ""
	}
}

// trafficSound reports whether an application's traffic is the 24 whole
// hours before the epoch's hour, in order, with totals that sum the set
// hours.
func trafficSound(epochAt time.Time, a *v1.Application) bool {
	tr := a.GetTraffic()
	if len(tr) != api.ApplicationHours {
		return false
	}
	var in int64
	set := false
	for i, h := range tr {
		if !h.GetHour().AsTime().Equal(epochAt.Truncate(time.Hour).Add(-time.Duration(api.ApplicationHours-i) * time.Hour)) {
			return false
		}
		if h.InMsgs != nil {
			in, set = in+h.GetInMsgs(), true
		}
	}
	return set == (a.InMsgs_24H != nil) && in == a.GetInMsgs_24H()
}

// applicationsRows are the Applications states both providers must show.
func applicationsRows() []State {
	return []State{
		{"an application grouped at the \"@\": its pods are its instances, on more than one server", application(func(_ *v1.GetApplicationsResponse, a *v1.Application) bool {
			var n int32
			for _, s := range a.GetServers() {
				n += s.GetConnections()
			}
			return a.GetPodsTotal() > 1 && slices.IsSorted(a.GetPods()) && a.GetInstances() >= a.GetPodsTotal() && a.GetConnections() >= a.GetInstances() &&
				len(a.GetServers()) > 1 && n == a.GetConnections()
		})},
		{"an application whose connections are its instances: no pod in their name", application(func(_ *v1.GetApplicationsResponse, a *v1.Application) bool {
			return a.GetPodsTotal() == 0 && a.GetConnections() > 1 && a.GetInstances() == a.GetConnections() && a.GetName() != api.UnnamedApplication
		})},
		{"the (unnamed) application", application(func(_ *v1.GetApplicationsResponse, a *v1.Application) bool {
			return a.GetName() == api.UnnamedApplication && a.GetConnections() > 0
		})},
		{"an application with more than one client library version", application(func(_ *v1.GetApplicationsResponse, a *v1.Application) bool {
			l := a.GetLibraries()
			return len(l) > 1 && l[0].GetConnections() >= l[1].GetConnections()
		})},
		{"applications first by an unmuted client finding on the application or one of its names", func(q Q) string {
			res := q.apps(q.day(), "", false)
			if len(res.GetApplications()) == 0 {
				return ""
			}
			a := res.GetApplications()[0]
			for _, f := range a.GetFindings() {
				if !f.GetMuted() && api.ApplicationOf(f.GetEntityKey()) == a.GetName() && f.GetId() == f.GetCheckCode()+":"+f.GetEntityKey() {
					return a.GetName() + " " + f.GetId()
				}
			}
			return ""
		}},
		{"an application that reconnected in the 24 hours, with its client-004 finding", application(func(_ *v1.GetApplicationsResponse, a *v1.Application) bool {
			return a.GetReconnects_24H() > 0 && a.GetStale_24H() > 0 && slices.ContainsFunc(a.GetFindings(), func(f *v1.FindingRef) bool {
				return f.GetCheckCode() == "client-004"
			})
		})},
		{"an application with slow consumer disconnects in the 24 hours", application(func(_ *v1.GetApplicationsResponse, a *v1.Application) bool {
			return a.GetSlowConsumers_24H() > 0
		})},
		{"an application with short-lived connections in the 24 hours and no reconnect", application(func(_ *v1.GetApplicationsResponse, a *v1.Application) bool {
			return a.GetShortLived_24H() > 0 && a.Reconnects_24H != nil && a.GetReconnects_24H() == 0
		})},
		{"the last day's counts summed over the applications", func(q Q) string {
			res := q.apps(q.day(), "", false)
			if !res.GetEventsCaptured() || res.GetApplicationsTotal() > int32(len(res.GetApplications())) {
				return ""
			}
			var slow, stale, rec int32
			for _, a := range res.GetApplications() {
				slow, stale = slow+a.GetSlowConsumers_24H(), stale+a.GetStale_24H()
				if a.GetReconnects_24H() > 0 {
					rec++
				}
			}
			d := res.GetLastDay()
			if d.GetSlowConsumers() != slow || d.GetStale() != stale || d.GetReconnecting() != rec {
				q.t.Errorf("%s: last day %v, rows %d %d %d", q.System, d, slow, stale, rec)
				return ""
			}
			if stale == 0 || rec == 0 {
				return ""
			}
			return fmt.Sprintf("%d stale, %d reconnecting", stale, rec)
		}},
		{"an application's 24 hours of traffic from the rollup, totals over the set hours", application(func(res *v1.GetApplicationsResponse, a *v1.Application) bool {
			return trafficSound(res.GetEpochAt().AsTime(), a) && a.GetInMsgs_24H() > 0 && a.GetOutBytes_24H() > 0
		})},
		{"an application with hours of traffic unset and hours set", application(func(res *v1.GetApplicationsResponse, a *v1.Application) bool {
			unset := slices.ContainsFunc(a.GetTraffic(), func(h *v1.HourTraffic) bool { return h.InMsgs == nil && h.OutBytes == nil })
			return unset && a.InMsgs_24H != nil && trafficSound(res.GetEpochAt().AsTime(), a)
		})},
		{"one server holding most connections, more than one used", func(q Q) string {
			sp := q.apps(q.day(), "", false).GetSpread()
			if sp.GetConcentrated() && 2*sp.GetConnections() > sp.GetTotal() && sp.GetServers() > 1 {
				return fmt.Sprintf("%s %d of %d", sp.GetServer(), sp.GetConnections(), sp.GetTotal())
			}
			return ""
		}},
		{"tooling connections hidden, and listed marked when included", func(q Q) string {
			hid := q.apps(q.day(), "", false)
			if hid.GetToolingHidden() == 0 || slices.ContainsFunc(hid.GetApplications(), (*v1.Application).GetTooling) {
				return ""
			}
			all := q.apps(q.day(), "", true)
			if all.GetToolingHidden() != 0 || all.GetConnectionsTotal() != hid.GetConnectionsTotal()+hid.GetToolingHidden() {
				q.t.Errorf("%s: %d connections and %d hidden; with tooling %d", q.System, hid.GetConnectionsTotal(), hid.GetToolingHidden(), all.GetConnectionsTotal())
				return ""
			}
			for _, a := range all.GetApplications() {
				if a.GetTooling() {
					return a.GetName()
				}
			}
			return ""
		}},
		{"an account scope: only its applications", func(q Q) string {
			res := q.apps(q.day(), "", false)
			var accounts []string
			for _, a := range res.GetApplications() {
				for _, acc := range a.GetAccounts() {
					if !slices.Contains(accounts, acc.GetAccount()) {
						accounts = append(accounts, acc.GetAccount())
					}
				}
			}
			if len(accounts) < 2 {
				return ""
			}
			slices.Sort(accounts)
			scoped := q.apps(q.day(), accounts[len(accounts)-1], false)
			if len(scoped.GetApplications()) == 0 || scoped.GetConnectionsTotal() >= res.GetConnectionsTotal() {
				return ""
			}
			for _, a := range scoped.GetApplications() {
				if !slices.ContainsFunc(a.GetAccounts(), func(x *v1.ApplicationAccount) bool { return x.GetAccount() == accounts[len(accounts)-1] }) {
					q.t.Errorf("%s: %s in scope %s with %v", q.System, a.GetName(), accounts[len(accounts)-1], a.GetAccounts())
					return ""
				}
			}
			return accounts[len(accounts)-1]
		}},
		{"an Applications page of a past range: an older collection", func(q Q) string {
			live := q.apps(q.day(), "", false)
			end := q.env.Now.Add(-3 * time.Hour)
			past := q.apps(&v1.TimeRange{Start: timestamppb.New(end.Add(-time.Hour)), End: timestamppb.New(end)}, "", false)
			if past.GetEpoch() == 0 || past.GetEpoch() >= live.GetEpoch() || past.GetEpochAt().AsTime().After(end) || len(past.GetApplications()) == 0 {
				return ""
			}
			for _, a := range past.GetApplications() {
				if !trafficSound(past.GetEpochAt().AsTime(), a) {
					q.t.Errorf("%s: %s's traffic at %v: %v", q.System, a.GetName(), past.GetEpochAt().AsTime(), a.GetTraffic())
					return ""
				}
			}
			return past.GetEpochAt().AsTime().Format(time.RFC3339)
		}},
	}
}
