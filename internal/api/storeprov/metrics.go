package storeprov

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
)

// MaxFindingSeries caps the unmuted per-entity series of one check in
// /metrics, keeping the worst and then by entity key; muted findings are
// always exported, and nats_lens_findings_by_check counts every entity.
const MaxFindingSeries = 50

// severityValue is a finding's severity as a number: 1 info, 2 warning,
// 3 critical.
func severityValue(s v1.Severity) float64 {
	switch s {
	case v1.Severity_SEVERITY_CRITICAL:
		return 3
	case v1.Severity_SEVERITY_WARNING:
		return 2
	}
	return 1
}

// WriteMetrics writes the Prometheus metrics: every firing finding with
// its mute, severity and flapping as separate series of one identity
// (code, entity type, entity key), so that a change of severity or a mute
// does not start a new series; per-check counts; counts by severity; and
// the health of the collector and the checks.
func (p *Provider) WriteMetrics(ctx context.Context, m *api.Exposition) error {
	now := p.Now()
	fs, err := p.St.Findings(ctx, now, store.FindingQuery{FiringOnly: true})
	if err != nil {
		return err
	}
	protos, err := p.findingProtos(ctx, fs)
	if err != nil {
		return err
	}
	// The cap keeps the worst entities, then by entity key, so the set of
	// series is stable from scrape to scrape whatever fired first.
	slices.SortStableFunc(protos, func(a, b *v1.Finding) int {
		return cmp.Or(cmp.Compare(a.GetCheckCode(), b.GetCheckCode()),
			cmp.Compare(severityValue(b.GetSeverity()), severityValue(a.GetSeverity())),
			cmp.Compare(a.GetEntity().GetKey(), b.GetEntity().GetKey()))
	})

	type series struct {
		labels   []string
		muted    bool
		severity float64
		flapping bool
	}
	var shown []series
	perCode := map[string]int{}
	omitted := map[string]int{}
	unmuted := map[string]int{}
	maxSev := map[string]float64{}
	muted := 0
	bySeverity := map[string]int{"critical": 0, "warning": 0, "info": 0}
	for _, f := range protos {
		code, sev := f.GetCheckCode(), severityValue(f.GetSeverity())
		if f.GetMute() != nil {
			muted++
		} else {
			bySeverity[strings.ToLower(strings.TrimPrefix(f.GetSeverity().String(), "SEVERITY_"))]++
			unmuted[code]++
			maxSev[code] = max(maxSev[code], sev)
		}
		// Muted findings are always exported and do not count against the
		// cap: the alerting rules need them to stop matching.
		if f.GetMute() == nil {
			if perCode[code] >= MaxFindingSeries {
				omitted[code]++
				continue
			}
			perCode[code]++
		}
		shown = append(shown, series{
			labels:   []string{"code", code, "entity_type", entityTypeName(f.GetEntity().GetType()), "entity_key", f.GetEntity().GetKey()},
			muted:    f.GetMute() != nil,
			severity: sev,
			flapping: f.GetState() == v1.FindingState_FINDING_STATE_FLAPPING,
		})
	}
	m.Family("nats_lens_finding", "gauge", "1 for each firing finding, muted or not; at most "+fmt.Sprint(MaxFindingSeries)+" unmuted entities per check, the worst then by key.")
	for _, s := range shown {
		m.Sample("nats_lens_finding", 1, append(s.labels, "muted", fmt.Sprint(s.muted))...)
	}
	m.Family("nats_lens_finding_severity", "gauge", "Severity of each firing finding: 1 info, 2 warning, 3 critical.")
	for _, s := range shown {
		m.Sample("nats_lens_finding_severity", s.severity, s.labels...)
	}
	m.Family("nats_lens_finding_flapping", "gauge", "1 for a firing finding that flaps, 0 otherwise.")
	for _, s := range shown {
		v := 0.0
		if s.flapping {
			v = 1
		}
		m.Sample("nats_lens_finding_flapping", v, s.labels...)
	}
	codes := make([]string, 0, len(unmuted))
	for c := range unmuted {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	m.Family("nats_lens_findings_by_check", "gauge", "Firing, unmuted findings per check, every entity counted.")
	for _, c := range codes {
		m.Sample("nats_lens_findings_by_check", float64(unmuted[c]), "code", c)
	}
	m.Family("nats_lens_findings_by_check_max_severity", "gauge", "Highest severity among a check's firing, unmuted findings: 1 info, 2 warning, 3 critical.")
	for _, c := range codes {
		m.Sample("nats_lens_findings_by_check_max_severity", maxSev[c], "code", c)
	}
	m.Family("nats_lens_finding_series_omitted", "gauge", "Firing findings of a check left out of the per-finding series by its cap.")
	omittedCodes := make([]string, 0, len(omitted))
	for c := range omitted {
		omittedCodes = append(omittedCodes, c)
	}
	sort.Strings(omittedCodes)
	for _, c := range omittedCodes {
		m.Sample("nats_lens_finding_series_omitted", float64(omitted[c]), "code", c)
	}
	m.Family("nats_lens_findings_total", "gauge", "Firing, unmuted findings by severity.")
	for _, sev := range []string{"critical", "warning", "info"} {
		m.Sample("nats_lens_findings_total", float64(bySeverity[sev]), "severity", sev)
	}
	m.Family("nats_lens_findings_muted", "gauge", "Firing findings an active mute covers.")
	m.Sample("nats_lens_findings_muted", float64(muted))

	if err := p.writeNotifications(ctx, m, now); err != nil {
		return err
	}
	return p.writeHealth(ctx, m, now)
}

// writeHealth writes the collector's and the checks' health.
func (p *Provider) writeHealth(ctx context.Context, m *api.Exposition, now time.Time) error {
	hourAgo := now.Add(-time.Hour)
	var last sql.NullTime
	var partial sql.NullBool
	var lastEpoch sql.NullInt64
	err := p.db().QueryRowContext(ctx, `SELECT epoch, started_at, partial FROM epochs ORDER BY epoch DESC LIMIT 1`).Scan(&lastEpoch, &last, &partial)
	if err != nil && !isNoRows(err) {
		return err
	}
	m.Family("nats_lens_last_epoch_age_seconds", "gauge", "Seconds since the newest stored epoch started; absent before the first epoch.")
	if last.Valid {
		m.Sample("nats_lens_last_epoch_age_seconds", now.Sub(last.Time.UTC()).Seconds())
	}
	m.Family("nats_lens_last_epoch_partial", "gauge", "1 when the newest epoch is partial: a server or an endpoint did not answer.")
	if partial.Valid {
		v := 0.0
		if partial.Bool {
			v = 1
		}
		m.Sample("nats_lens_last_epoch_partial", v)
	}
	var partials, epochs, dropped, runErrors int64
	if err := p.db().QueryRowContext(ctx, `SELECT count(*) FILTER (WHERE partial), count(*) FROM epochs WHERE started_at >= ?`, hourAgo).
		Scan(&partials, &epochs); err != nil {
		return err
	}
	m.Family("nats_lens_epochs_last_hour", "gauge", "Epochs stored in the last hour.")
	m.Sample("nats_lens_epochs_last_hour", float64(epochs))
	m.Family("nats_lens_partial_epochs_last_hour", "gauge", "Partial epochs stored in the last hour.")
	m.Sample("nats_lens_partial_epochs_last_hour", float64(partials))
	if err := p.db().QueryRowContext(ctx, `SELECT coalesce(sum(coalesce(TRY_CAST(json_extract_string(raw, '$.dropped') AS BIGINT), 1)), 0)
FROM events WHERE kind = 'collector.messages_dropped' AND received_at >= ?`, hourAgo).Scan(&dropped); err != nil {
		return err
	}
	m.Family("nats_lens_event_messages_dropped_last_hour", "gauge", "Event messages the collector dropped in the last hour.")
	m.Sample("nats_lens_event_messages_dropped_last_hour", float64(dropped))
	if err := p.db().QueryRowContext(ctx, `SELECT count(*) FROM check_runs r JOIN epochs e USING (epoch)
WHERE r.status = 'error' AND e.started_at >= ?`, hourAgo).Scan(&runErrors); err != nil {
		return err
	}
	// Epochs whose checks have not been stored: the newest interval is left
	// out, as its checks may still be running.
	var frozen int64
	if err := p.db().QueryRowContext(ctx, `SELECT count(*) FROM epochs e WHERE e.started_at >= ? AND e.started_at < ?
AND NOT EXISTS (SELECT 1 FROM check_runs r WHERE r.epoch = e.epoch)`, hourAgo, now.Add(-max(p.Interval, time.Second))).Scan(&frozen); err != nil {
		return err
	}
	m.Family("nats_lens_epochs_without_check_runs_last_hour", "gauge", "Epochs of the last hour, the newest interval left out, for which no check ran.")
	m.Sample("nats_lens_epochs_without_check_runs_last_hour", float64(frozen))
	m.Family("nats_lens_check_run_errors_last_hour", "gauge", "Check runs that failed in the last hour.")
	m.Sample("nats_lens_check_run_errors_last_hour", float64(runErrors))
	m.Family("nats_lens_check_failing", "gauge", "1 for a check whose run in the newest epoch failed.")
	if lastEpoch.Valid {
		rows, err := p.db().QueryContext(ctx, `SELECT check_code FROM check_runs WHERE epoch = ? AND status = 'error' ORDER BY check_code`, lastEpoch.Int64)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var code string
			if err := rows.Scan(&code); err != nil {
				return err
			}
			m.Sample("nats_lens_check_failing", 1, "code", code)
		}
		if err := rows.Err(); err != nil {
			return err
		}
	}
	return nil
}

// writeNotifications reads durable outcomes; queue age is not delivery time.
func (p *Provider) writeNotifications(ctx context.Context, m *api.Exposition, now time.Time) error {
	rows, err := p.db().QueryContext(ctx, `SELECT channel,
 count(*) FILTER (WHERE state='pending'),
 count(*) FILTER (WHERE state='delivered' AND finished_at >= make_timestamp(?::BIGINT) AND finished_at <= make_timestamp(?::BIGINT)),
 count(*) FILTER (WHERE state IN ('refused','failed') AND finished_at >= make_timestamp(?::BIGINT) AND finished_at <= make_timestamp(?::BIGINT)),
 min(created_at) FILTER (WHERE state='pending')
 FROM notify_outbox GROUP BY channel ORDER BY channel`, now.Add(-time.Hour).UnixMicro(), now.UnixMicro(), now.Add(-time.Hour).UnixMicro(), now.UnixMicro())
	if err != nil {
		return err
	}
	defer rows.Close()
	for _, family := range []struct{ name, help string }{{"pending", "Notifications pending delivery."}, {"delivered_last_hour", "Notifications delivered in the last hour."}, {"failed_last_hour", "Notifications refused or given up in the last hour."}, {"oldest_pending_seconds", "Seconds since the oldest pending notification was queued; absent with none pending."}} {
		m.Family("nats_lens_notifications_"+family.name, "gauge", family.help)
	}
	for rows.Next() {
		var channel string
		var pending, delivered, failed int64
		var oldest sql.NullTime
		if err := rows.Scan(&channel, &pending, &delivered, &failed, &oldest); err != nil {
			return err
		}
		m.Sample("nats_lens_notifications_pending", float64(pending), "channel", channel)
		m.Sample("nats_lens_notifications_delivered_last_hour", float64(delivered), "channel", channel)
		m.Sample("nats_lens_notifications_failed_last_hour", float64(failed), "channel", channel)
		if oldest.Valid {
			m.Sample("nats_lens_notifications_oldest_pending_seconds", max(0, now.Sub(oldest.Time.UTC()).Seconds()), "channel", channel)
		}
	}
	return rows.Err()
}
