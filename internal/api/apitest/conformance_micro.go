package apitest

import (
	"fmt"
	"math"
	"slices"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// micro reads the Micro services page of a range in an account scope.
func (q Q) micro(r *v1.TimeRange, account string) *v1.GetMicroServicesResponse {
	q.t.Helper()
	return must(q.env.Client.GetMicroServices(ctx, connect.NewRequest(&v1.GetMicroServicesRequest{System: q.System, Range: r,
		Account: account}))).of(q.t)
}

// microHours are the day's ranges of an hour, the newest first.
func (q Q) microHours() []*v1.TimeRange {
	var out []*v1.TimeRange
	for h := range 24 {
		end := q.env.Now.Add(-time.Duration(h) * time.Hour)
		out = append(out, &v1.TimeRange{Start: timestamppb.New(end.Add(-time.Hour)), End: timestamppb.New(end)})
	}
	return out
}

// microService finds a service of the day's page that ok accepts.
func microService(ok func(*v1.MicroService) bool) func(Q) string {
	return func(q Q) string {
		for _, s := range q.micro(q.day(), "").GetServices() {
			if ok(s) {
				return s.GetAccount() + "/" + s.GetName()
			}
		}
		return ""
	}
}

// microEndpoint finds an endpoint of the day's page that ok accepts.
func microEndpoint(ok func(*v1.MicroService, *v1.MicroEndpoint) bool) func(Q) string {
	return func(q Q) string {
		for _, s := range q.micro(q.day(), "").GetServices() {
			for _, e := range s.GetEndpoints() {
				if ok(s, e) {
					return s.GetName() + "." + e.GetName()
				}
			}
		}
		return ""
	}
}

func hasInstance(s *v1.MicroService, id string) bool {
	return slices.ContainsFunc(s.GetInstanceList(), func(in *v1.MicroInstance) bool { return in.GetId() == id })
}

// microRows are the Micro services states both providers must show.
func microRows() []State {
	return []State{
		{"a Micro services service that answered, with its versions, endpoints and rate", microService(func(s *v1.MicroService) bool {
			return s.GetAsked() && s.GetAnswered() && s.GetInstanceSource() == v1.MicroInstanceSource_MICRO_INSTANCE_SOURCE_ANSWERED && len(s.GetVersions()) > 0 && s.GetDescription() != "" && s.RequestsPerSecond != nil &&
				len(s.GetEndpoints()) > 0 && s.GetEndpoints()[0].GetSubject() != "" && s.GetEndpoints()[0].GetQueueGroup() != "" &&
				s.GetInstances() == int32(len(s.GetInstanceList())) && s.GetInstanceList()[0].GetStarted() != nil
		})},
		{"a Micro services service seen in subscriptions only, its account not asked", microService(func(s *v1.MicroService) bool {
			return !s.GetAsked() && !s.GetAnswered() && s.GetNote() == v1.MicroNote_MICRO_NOTE_NOT_ASKED && s.GetInstances() > 0 &&
				s.GetInstanceSource() == v1.MicroInstanceSource_MICRO_INSTANCE_SOURCE_SUBSCRIPTIONS &&
				len(s.GetConnectionNames()) > 0 && len(s.GetEndpoints()) == 0 && len(s.GetVersions()) == 0 && s.RequestsPerSecond == nil
		})},
		{"a Micro services service with mixed versions", microService(func(s *v1.MicroService) bool {
			var n int32
			for _, v := range s.GetVersions() {
				n += v.GetInstances()
			}
			return len(s.GetVersions()) > 1 && n <= s.GetInstances() && s.GetVersions()[0].GetInstances() >= s.GetVersions()[1].GetInstances() &&
				s.GetNote() == v1.MicroNote_MICRO_NOTE_MIXED_VERSIONS
		})},
		{"a Micro services instance missing, in an hour of the day", func(q Q) string {
			for _, r := range q.microHours() {
				res := q.micro(r, "")
				for _, s := range res.GetServices() {
					for _, m := range s.GetMissing() {
						if hasInstance(s, m.GetId()) || !m.GetSince().AsTime().After(m.GetLastSeen().AsTime()) ||
							m.GetLastSeen().AsTime().Before(res.GetEpochAt().AsTime().Add(-api.MicroMissingWindow)) ||
							s.GetNote() != v1.MicroNote_MICRO_NOTE_INSTANCE_MISSING {
							q.t.Errorf("%s: %s missing %v", q.System, s.GetName(), m)
							return ""
						}
						return fmt.Sprintf("%s %s before %s", s.GetName(), m.GetId(), r.GetEnd().AsTime().Format("15:04"))
					}
				}
			}
			return ""
		}},
		{"a Micro services rate over an instance restart in the rate window, as before the restart", func(q Q) string {
			res := q.micro(q.day(), "")
			from := res.GetEpochAt().AsTime().Add(-api.RateSpan)
			for _, s := range res.GetServices() {
				for _, in := range s.GetInstanceList() {
					if in.GetStarted() == nil || !in.GetStarted().AsTime().After(from) {
						continue
					}
					// The page before the restart: a gone instance's run
					// counted with its replacement's would read high.
					at := in.GetStarted().AsTime()
					before := q.micro(&v1.TimeRange{Start: timestamppb.New(at.Add(-time.Hour)), End: timestamppb.New(at)}, s.GetAccount())
					var b *v1.MicroService
					for _, x := range before.GetServices() {
						if x.GetName() == s.GetName() {
							b = x
						}
					}
					if b == nil || b.RequestsPerSecond == nil || math.Abs(s.GetRequestsPerSecond()/b.GetRequestsPerSecond()-1) > 0.1 {
						q.t.Errorf("%s: %s restarted, rate %.2f, before %.2f", q.System, s.GetName(), s.GetRequestsPerSecond(), b.GetRequestsPerSecond())
						return ""
					}
					return fmt.Sprintf("%s %s: %.1f, before %.1f", s.GetName(), in.GetId(), s.GetRequestsPerSecond(), b.GetRequestsPerSecond())
				}
			}
			return ""
		}},
		{"a Micro services account with its readable name, on its services too", func(q Q) string {
			res := q.micro(q.day(), "")
			for _, a := range res.GetAccounts() {
				if a.GetAccountName() == "" {
					continue
				}
				for _, s := range res.GetServices() {
					if s.GetAccount() == a.GetAccount() && s.GetAccountName() != a.GetAccountName() {
						q.t.Errorf("%s: %s named %q, its service %s %q", q.System, a.GetAccount(), a.GetAccountName(), s.GetName(), s.GetAccountName())
						return ""
					}
				}
				return a.GetAccount() + " " + a.GetAccountName()
			}
			return ""
		}},
		{"a Micro services endpoint with its average time and error share", microEndpoint(func(_ *v1.MicroService, e *v1.MicroEndpoint) bool {
			return e.AverageMs != nil && e.GetAverageMs() > 0 && e.ErrorShare != nil && e.GetErrorShare() > 0 && e.GetErrorShare() <= 1 &&
				e.GetRequestsPerSecond() > 0
		})},
		{"a Micro services slow endpoint, its 7-day level and the start of its slow run", microEndpoint(func(s *v1.MicroService, e *v1.MicroEndpoint) bool {
			return e.GetSlow() && e.LevelMs != nil && e.GetAverageMs() >= api.MicroSlowFactor*e.GetLevelMs() &&
				e.GetAverageMs() >= e.GetLevelMs()+api.MicroSlowMarginMs && e.GetSlowSince() != nil && s.GetEndpoints()[0] == e
		})},
		{"a Micro services endpoint that is not slow, with its level", microEndpoint(func(_ *v1.MicroService, e *v1.MicroEndpoint) bool {
			return !e.GetSlow() && e.LevelMs != nil && e.AverageMs != nil && e.GetSlowSince() == nil
		})},
		{"a Micro services endpoint's last error with the instance that reported it", microEndpoint(func(s *v1.MicroService, e *v1.MicroEndpoint) bool {
			return e.GetLastError() != "" && hasInstance(s, e.GetLastErrorInstance())
		})},
		{"a Micro services $SRV.STATS timeout, the instances still subscribed: seen in subscriptions, not missing", func(q Q) string {
			for _, r := range q.microHours() {
				res := q.micro(r, "")
				timedOut := map[string]bool{}
				for _, a := range res.GetAccounts() {
					timedOut[a.GetAccount()] = a.GetStats().GetState() == v1.MicroRequestState_MICRO_REQUEST_STATE_TIMEOUT
				}
				for _, s := range res.GetServices() {
					if !timedOut[s.GetAccount()] || s.GetInstances() == 0 {
						continue
					}
					if s.GetAnswered() || len(s.GetMissing()) != 0 || s.GetNote() != v1.MicroNote_MICRO_NOTE_NO_ANSWER ||
						s.GetInstanceSource() != v1.MicroInstanceSource_MICRO_INSTANCE_SOURCE_SUBSCRIPTIONS {
						q.t.Errorf("%s: %s after a timeout: %v", q.System, s.GetName(), s)
						return ""
					}
					return fmt.Sprintf("%s before %s", s.GetName(), r.GetEnd().AsTime().Format("15:04"))
				}
			}
			return ""
		}},
		{"a Micro services account asked without an answer", func(q Q) string {
			for _, a := range q.micro(q.day(), "").GetAccounts() {
				if a.GetAsked() && a.GetStats().GetState() != v1.MicroRequestState_MICRO_REQUEST_STATE_OK && a.GetStats().GetAt() != nil {
					return a.GetAccount() + " " + a.GetStats().GetState().String()
				}
			}
			return ""
		}},
		{"the Micro services order, counts and scope, in every system", everySystem(func(q Q) string {
			res := q.micro(q.day(), "")
			if res.GetServicesTotal() < int32(len(res.GetServices())) || len(res.GetServices()) > api.MaxMicroServices {
				q.t.Errorf("%s: %d services, total %d", q.System, len(res.GetServices()), res.GetServicesTotal())
				return ""
			}
			var counted int32
			for _, a := range res.GetAccounts() {
				counted += a.GetServices()
				scoped := q.micro(q.day(), a.GetAccount())
				if scoped.GetServicesTotal() != a.GetServices() || len(scoped.GetAccounts()) != 1 {
					q.t.Errorf("%s: %s scoped has %d services, %d accounts; the account says %d", q.System, a.GetAccount(),
						scoped.GetServicesTotal(), len(scoped.GetAccounts()), a.GetServices())
					return ""
				}
			}
			if counted != res.GetServicesTotal() {
				q.t.Errorf("%s: accounts count %d services, total %d", q.System, counted, res.GetServicesTotal())
				return ""
			}
			for i, s := range res.GetServices() {
				if i > 0 && api.MicroNoteWarns(s.GetNote()) && !api.MicroNoteWarns(res.GetServices()[i-1].GetNote()) {
					q.t.Errorf("%s: %s warns after a service that does not", q.System, s.GetName())
				}
				if s.GetNote() == v1.MicroNote_MICRO_NOTE_UNSPECIFIED || len(s.GetEndpoints()) > api.MaxMicroEndpoints ||
					s.GetEndpointsTotal() < int32(len(s.GetEndpoints())) || len(s.GetConnectionNames()) > api.MaxClientNames {
					q.t.Errorf("%s: service %v", q.System, s)
				}
			}
			if res.GetEpoch() == 0 || len(res.GetServices()) == 0 {
				return ""
			}
			return fmt.Sprintf("%d services", len(res.GetServices()))
		})},
		{"the Micro services instances run on connections of the Connections page, their $SRV subscriptions on the Subjects page, in every system", everySystem(func(q Q) string {
			res := q.micro(q.day(), "")
			if res.SubscriptionsAt == nil {
				return ""
			}
			names := map[string]bool{}
			conns := must(q.env.Client.ListConnections(ctx, connect.NewRequest(&v1.ListConnectionsRequest{System: q.System, Range: q.day(),
				IncludeTooling: true, PageSize: 1000}))).of(q.t)
			for _, c := range conns.GetConnections() {
				names[c.GetServer()+"/"+c.GetName()] = true
			}
			n := 0
			for _, s := range res.GetServices() {
				for _, in := range s.GetInstanceList() {
					if in.GetConnectionName() != "" && !names[in.GetServer()+"/"+in.GetConnectionName()] && conns.GetNextPageToken() == "" {
						q.t.Errorf("%s: %s on %s/%s, no such connection", q.System, in.GetId(), in.GetServer(), in.GetConnectionName())
						return ""
					}
				}
				i := slices.IndexFunc(s.GetInstanceList(), func(in *v1.MicroInstance) bool { return in.GetConnectionName() != "" })
				if i < 0 {
					continue
				}
				id := s.GetInstanceList()[i].GetId()
				m := q.match(s.GetAccount(), api.SrvPrefix+"PING."+s.GetName()+"."+id)
				found := false
				for _, c := range api.MatchedSubscribers(m) {
					found = found || c.GetSubject() == api.SrvPrefix+"PING."+s.GetName()+"."+id
				}
				if !found {
					q.t.Errorf("%s: no $SRV subscription of %s/%s", q.System, s.GetName(), id)
					return ""
				}
				n++
			}
			if n == 0 {
				return ""
			}
			return fmt.Sprintf("%d services", n)
		})},
	}
}

func closeTo(a, b float64) bool { return a-b < 1e-6*max(1, b) && b-a < 1e-6*max(1, b) }
