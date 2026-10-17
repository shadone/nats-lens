package fake

import (
	"cmp"
	"slices"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

const (
	firing   = v1.FindingState_FINDING_STATE_FIRING
	flapping = v1.FindingState_FINDING_STATE_FLAPPING
	resolved = v1.FindingState_FINDING_STATE_RESOLVED
	critical = v1.Severity_SEVERITY_CRITICAL
	warning  = v1.Severity_SEVERITY_WARNING
	info     = v1.Severity_SEVERITY_INFO
)

// cfg builds related configuration; keys in hl are highlighted.
func cfg(pairs [][2]string, hl ...string) []*v1.ConfigValue {
	out := make([]*v1.ConfigValue, len(pairs))
	for i, p := range pairs {
		out[i] = &v1.ConfigValue{Key: p[0], Value: p[1], Highlight: slices.Contains(hl, p[0])}
	}
	return out
}

func finding(code, name string, sev v1.Severity, state v1.FindingState, e *v1.EntityRef, since, changed time.Time, detail string) *v1.Finding {
	return &v1.Finding{
		Id: code + ":" + e.GetKey(), CheckCode: code, CheckName: name, Severity: sev, State: state, Entity: e,
		Since: ts(since), TransitionedAt: ts(changed), Detail: detail, TransitionsLastHour: 0,
	}
}

func resolvedFinding(code, name string, sev v1.Severity, e *v1.EntityRef, since, end time.Time, detail string) *v1.Finding {
	f := finding(code, name, sev, resolved, e, since, end, detail)
	f.ResolvedAt = ts(end)
	return f
}

// allFindings is every finding of the scenario, before mutes are applied.
func (s scen) allFindings() []*v1.Finding {
	c006 := finding("consumer-006", "Expected durable missing", critical, firing,
		consumerRef("order-archive", "analytics-aggregator"), s.at(9, 31, 7), s.at(9, 31, 7),
		"Listed in inventory/prod.yaml, absent on nats-0, nats-1 and nats-2. Deleted 09:31:07 by analytics-worker@10.20.0.12.")
	c006.Value = &v1.FindingValue{Name: "consumer present on assigned peers", Display: "0 / 3", Number: ptr(0.0),
		Basis: "per epoch, compared with inventory/prod.yaml"}
	c006.Remediation = "Recreate the durable from the inventory definition (`nats consumer add order-archive --config inventory/prod/analytics-aggregator.json`). " +
		"Then stop the worker from deleting it: analytics-worker calls `consumers.delete()` in its SIGTERM drain path, so every pod restart removes the shared durable."
	c006.RelatedConfig = cfg([][2]string{
		{"durable_name", "analytics-aggregator"}, {"ack_policy", "explicit"}, {"max_ack_pending", "8000"},
		{"ack_wait", "60s"}, {"max_deliver", "5"}, {"filter_subject", "orders.archive.>"},
	}, "max_ack_pending", "ack_wait")
	c006.RelatedConfigSource = "inventory"
	c006.Cause = &v1.EventRef{Id: idConsumerDeleted, Time: ts(s.at(9, 31, 7)), Kind: "consumer.deleted",
		Summary: "analytics-aggregator on order-archive by analytics-worker@10.20.0.12"}

	c012 := finding("consumer-012", "Creation time differs across servers", warning, firing,
		consumerRef("order-jobs", "resize-worker"), s.at(9, 45, 0), s.at(9, 45, 0),
		"nats-0 2026-08-12T10:02:11Z; nats-1, nats-2 2026-10-17T09:38:44Z. Recreated during the nats-1 restart.")
	c012.Remediation = "Compare the consumer on each server (`nats consumer info order-jobs resize-worker --server nats-0`). " +
		"If nats-0 holds the old copy, step the consumer leader down so the replicas resynchronise."

	s020 := finding("stream-020", "Backlog growing", warning, flapping,
		consumerRef("media-windows", "media-chunk-pool"), s.at(11, 0, 0), s.at(14, 12, 0),
		"Fill 42 msg/s, drain 31 msg/s over 1h. 18,420 pending, up 9,600 since 11:00.")
	s020.Value = &v1.FindingValue{Name: "fill minus drain", Display: "+11 msg/s", Number: ptr(11.0), Basis: "1h window, leader view"}
	s020.TransitionsLastHour = 4
	s020.Remediation = "Add workers to the media-chunk-pool pull consumer or lower the producer rate; the backlog grows since the 11:02 media-worker rollout."

	s025 := finding("stream-025", "Stream without limits", info, firing, streamRef("order-archive"),
		s.on(14, 14, 30), s.on(14, 14, 30), "max_bytes -1, max_msgs -1, max_age 0. Grows 96 MiB/day; full in 4.1 years at this rate.")
	s025.RelatedConfig = cfg([][2]string{{"max_bytes", "-1"}, {"max_msgs", "-1"}, {"max_age", "0"}}, "max_bytes")
	s025.RelatedConfigSource = "nats-0"

	s031 := finding("stream-031", "Days until full below 60", warning, firing, streamRef("audit-archive"),
		s.on(15, 8, 0), s.on(15, 8, 0), "41 days at 18 MiB/day against max_bytes 1.8 GiB.")

	out := []*v1.Finding{c006, c012, s020, s025, s031,
		resolvedFinding("consumer-008", "Consumer quorum lost", critical,
			consumerRef("order-jobs", "job-cache-projector"), s.at(9, 15, 0), s.at(9, 45, 0),
			"job-cache-projector back to 3 of 3 peers after 30m."),
		resolvedFinding("server-002", "Healthz failing", critical, serverRef("nats-0"), s.at(9, 16, 2), s.at(9, 18, 40), "nats-0 healthz unavailable while restarting."),
		resolvedFinding("server-002", "Healthz failing", critical, serverRef("nats-1"), s.at(9, 29, 5), s.at(9, 31, 40), "nats-1 healthz unavailable while restarting."),
		resolvedFinding("server-002", "Healthz failing", critical, serverRef("nats-2"), s.at(9, 41, 10), s.at(9, 44, 50), "nats-2 healthz unavailable while restarting."),
		resolvedFinding("stream-007", "Stream quorum lost", critical, streamRef("media-windows"), s.at(9, 16, 30), s.at(9, 35, 0), "1 of 3 peers."),
		resolvedFinding("stream-007", "Stream quorum lost", critical, streamRef("order-jobs"), s.at(9, 41, 30), s.at(9, 44, 30), "1 of 3 peers."),
		resolvedFinding("server-006", "Meta leader changed", warning, systemRef(), s.at(9, 16, 10), s.at(9, 31, 0), "Meta leader moved nats-0 to nats-1 to nats-0."),
		resolvedFinding("stream-013", "Replica lagging", warning, streamRef("order-archive"), s.at(9, 16, 40), s.at(10, 5, 0), "nats-1 replica 12,400 entries behind."),
		resolvedFinding("client-003", "Slow consumer events", warning, clientRef("media-worker"), s.at(9, 15, 30), s.at(9, 45, 0), "6 slow consumer disconnects in 30m."),
	}
	return out
}

// findings is every finding with the active mutes applied, worst first:
// unmuted before muted, then by severity, then the longest firing first.
func (p *Provider) findings(s scen) []*v1.Finding {
	all := s.allFindings()
	p.mu.Lock()
	for _, f := range all {
		if f.GetState() == resolved {
			continue
		}
		for _, m := range p.mutes {
			if m.active(s) && m.covers(f) {
				f.Mute = m.view(s)
				break
			}
		}
	}
	p.mu.Unlock()
	slices.SortStableFunc(all, func(a, b *v1.Finding) int {
		return cmp.Or(
			cmp.Compare(boolRank(a.GetState() == resolved), boolRank(b.GetState() == resolved)),
			cmp.Compare(boolRank(a.GetMute() != nil), boolRank(b.GetMute() != nil)),
			cmp.Compare(severityRank(a.GetSeverity()), severityRank(b.GetSeverity())),
			a.GetSince().AsTime().Compare(b.GetSince().AsTime()),
		)
	})
	return all
}

func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
}

// current returns the findings that fire at the range's end.
func current(all []*v1.Finding, r *v1.TimeRange) []*v1.Finding {
	var out []*v1.Finding
	for _, f := range all {
		if f.GetState() != resolved && f.GetSince().AsTime().Before(r.GetEnd().AsTime()) {
			out = append(out, f)
		}
	}
	return out
}

func tally(all []*v1.Finding, r *v1.TimeRange) *v1.FindingTally {
	t := &v1.FindingTally{}
	end := r.GetEnd().AsTime()
	for _, f := range all {
		if f.GetState() == resolved {
			if at := f.GetResolvedAt().AsTime(); !at.After(end) && at.After(end.Add(-24*time.Hour)) {
				t.Resolved_24H++
			}
			continue
		}
		if !f.GetSince().AsTime().Before(end) {
			continue
		}
		if f.GetState() == flapping {
			t.Flapping++
		}
		switch {
		case f.GetMute() != nil:
			t.Muted++
		case f.GetSeverity() == critical:
			t.Critical++
		case f.GetSeverity() == warning:
			t.Warning++
		case f.GetSeverity() == info:
			t.Info++
		}
	}
	return t
}
