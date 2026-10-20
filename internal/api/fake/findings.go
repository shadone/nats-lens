package fake

import (
	"cmp"
	"slices"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

const (
	firing   = v1.FindingState_FINDING_STATE_FIRING
	changed  = v1.FindingState_FINDING_STATE_CHANGED
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

// prodFindings is every finding of example-prod, before mutes are applied.
// Each one is a real check, with the detail its query writes.
func prodFindings(s scen) []*v1.Finding {
	c010 := finding("consumer-010", "Expected consumer missing", critical, firing,
		consumerRef("order-archive", "analytics-aggregator"), s.at(9, 31, 7), s.at(9, 31, 7),
		"Consumer analytics-aggregator on order-archive is in the inventory and no server reports it; "+
			"last reported by nats-0, nats-1, nats-2 at 2026-10-17 09:30:58 UTC. "+
			"Deleted 2026-10-17 09:31:07 UTC by analytics-worker (10.20.0.12).")
	c010.Remediation = "Recreate the durable from the inventory definition (`nats consumer add order-archive --config inventory/prod/analytics-aggregator.json`). " +
		"Then stop the worker from deleting it: analytics-worker calls `consumers.delete()` in its SIGTERM drain path, so every pod restart removes the shared durable."
	c010.RelatedConfig = cfg([][2]string{
		{"durable_name", "analytics-aggregator"}, {"ack_wait", "1m"}, {"max_deliver", "5"},
		{"max_ack_pending", "8000"}, {"filter_subjects", "orders.archive.>"}, {"replicas", "3"},
	}, "max_ack_pending", "ack_wait")
	c010.RelatedConfigSource = "inventory"
	c010.Cause = &v1.EventRef{Id: s.sc.eventID("consumer.deleted", fixed(9, 31, 7)), Time: ts(s.at(9, 31, 7)), Kind: "consumer.deleted",
		Summary: "analytics-aggregator on order-archive by analytics-worker@10.20.0.12"}

	c009 := finding("consumer-009", "Consumer creation time differs across servers", warning, firing,
		consumerRef("order-jobs", "resize-worker"), s.at(9, 45, 0), s.at(9, 45, 0),
		"nats-0 2026-08-12T10:02:11Z; nats-1, nats-2 2026-10-17T09:38:44Z. Delivered and acknowledged positions differ: "+
			"nats-0 delivered 40112 ack floor 40096, nats-1 delivered 48207 ack floor 48190, nats-2 delivered 48207 ack floor 48188.")
	c009.Value = &v1.FindingValue{Name: "distinct creation times", Display: "2", Number: ptr(2.0), Basis: "creation times the servers report"}
	c009.Remediation = "Compare the consumer on each server (`nats consumer info order-jobs resize-worker --server nats-0`). " +
		"If nats-0 holds the old copy, step the consumer leader down so the replicas resynchronise."

	c002 := finding("consumer-002", "Consumer backlog growing", warning, flapping,
		consumerRef("media-windows", "media-chunk-pool"), s.at(11, 0, 0), s.at(14, 12, 0),
		"Fill 37.4 msg/s, drain 31.6 msg/s over 00:30:00. 18420 pending, up 10440 since 14:00 UTC.")
	c002.Value = &v1.FindingValue{Name: "backlog growth, messages per second", Display: "5.8", Number: ptr(5.8),
		Basis: "growth of num_pending over the lookback"}
	c002.TransitionsLastHour = 4
	c002.Remediation = "Add workers to the media-chunk-pool pull consumer or lower the producer rate; the backlog grows since the 11:02 media-worker rollout."

	s005 := finding("stream-005", "Stream without any limit", info, firing, streamRef("order-archive"),
		s.on(14, 14, 30), s.on(14, 14, 30), "max_msgs -1, max_bytes -1, max_age 0: nothing bounds the stream.")
	s005.RelatedConfig = cfg([][2]string{{"max_msgs", "-1"}, {"max_bytes", "-1"}, {"max_age", "0"},
		{"max_msgs_per_subject", "-1"}, {"allow_msg_ttl", "false"}}, "max_bytes")
	s005.RelatedConfigSource = "nats-0"

	s006 := finding("stream-006", "Stream reaches its byte limit soon", warning, firing, streamRef("audit-archive"),
		s.on(15, 8, 0), s.on(15, 8, 0),
		"Grows 18.0 MiB per day over the last 6h: reaches max_bytes 1.3 GiB in 13.1 days (1.1 GiB now); discard new.")
	s006.Value = &v1.FindingValue{Name: "days until the byte limit", Display: "13.1", Number: ptr(13.1),
		Basis: "days until max_bytes at the current growth"}
	s006.RelatedConfig = cfg([][2]string{{"max_bytes", "1395864371"}, {"discard", "new"}}, "max_bytes")

	healthz := func(server, stream string, since, end time.Time) *v1.Finding {
		return resolvedFinding("server-002", "Healthz not ok", critical, serverRef(server), since, end,
			server+" healthz unavailable (503): JetStream stream 'APP > "+stream+"' is not current (+2 more).")
	}
	out := []*v1.Finding{c010, c009, c002, s005, s006,
		shortLived("stock-poller", s.at(12, 48, 0), 21, []string{"10.20.4.31"}, []string{"nats-0", "nats-1"}),
		resolvedFinding("consumer-001", "Consumer quorum lost", critical,
			consumerRef("order-jobs", "job-cache-projector"), s.at(9, 15, 0), s.at(9, 45, 0),
			"Leader nats-0 sees 1 of 3 replicas online; quorum needs 2, for 60 collections."),
		healthz("nats-0", "media-windows", s.at(9, 16, 2), s.at(9, 18, 40)),
		healthz("nats-1", "order-jobs", s.at(9, 29, 5), s.at(9, 31, 40)),
		healthz("nats-2", "order-jobs", s.at(9, 41, 10), s.at(9, 44, 50)),
		resolvedFinding("stream-001", "Stream quorum lost", critical, streamRef("media-windows"), s.at(9, 16, 30), s.at(9, 35, 0),
			"Leader nats-1 sees 1 of 3 replicas online; quorum needs 2, for 37 collections."),
		resolvedFinding("stream-001", "Stream quorum lost", critical, streamRef("order-jobs"), s.at(9, 41, 30), s.at(9, 44, 30),
			"Leader nats-0 sees 1 of 3 replicas online; quorum needs 2, for 6 collections."),
		resolvedFinding("stream-003", "Stream replica lagging", warning, streamRef("order-archive"), s.at(9, 16, 40), s.at(10, 5, 0),
			"Leader nats-2 reports nats-1 12400 operations behind."),
		// It fired at the third disconnect and resolved 30 minutes after
		// the last (prodSlowConsumerEvents).
		resolvedFinding("client-003", "Slow consumer disconnects", warning, clientRef("media-worker"), s.at(9, 17, 30), s.at(10, 2, 0),
			prodSlowDetail(6, "nats-0, nats-1, nats-2", s.at(9, 32, 0))),
	}
	return out
}

// findings is every finding with the active mutes applied, worst first:
// unmuted before muted, then by severity, then the longest firing first.
func (p *Provider) findings(s scen) []*v1.Finding {
	all := s.sc.findings(s)
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
