package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Check run statuses (check_runs.status).
const (
	RunOK       = "ok"
	RunError    = "error"
	RunDisabled = "disabled"
)

// Finding states and transition kinds.
const (
	StateFiring      = "firing"
	StateResolved    = "resolved"
	TransitionFiring = "firing"
	// TransitionChanged is a change of severity while firing.
	TransitionChanged  = "changed"
	TransitionResolved = "resolved"
)

// FlappingAbove is the number of cycles (resolved transitions) in an hour
// above which a finding is flapping: it fired and resolved over and over.
const FlappingAbove = 3

// FlappingWindow is the window flapping counts transitions in.
const FlappingWindow = time.Hour

// CheckRun is one check's outcome for one epoch, as the checks engine
// hands it to ApplyCheckRuns.
type CheckRun struct {
	Code       string
	EntityType string
	// Status is RunOK, RunError or RunDisabled.
	Status   string
	Duration time.Duration
	// Error is the failure text for RunError.
	Error string
	// Rows are the failing entities; only read for RunOK.
	Rows []FindingRow
}

// FindingRow is one failing entity returned by a check.
type FindingRow struct {
	EntityKey string
	// Severity is info, warning or critical.
	Severity string
	Detail   string
	// Value is the check's number for the entity, when it returns one.
	Value *float64
	// CauseEventID is the events.id of the likely cause; 0 when none.
	CauseEventID int64
	// RelatedConfig maps setting names to values as text.
	RelatedConfig map[string]string
	// RelatedSource says where RelatedConfig comes from.
	RelatedSource string
	// ConditionSince is when the condition began, when the check knows it.
	ConditionSince *time.Time
}

// ApplyResult counts the transitions one ApplyCheckRuns call wrote.
type ApplyResult struct {
	Firing, Changed, Resolved int
	// DroppedValues names, sorted, the findings whose value was not a finite
	// number and was stored as NULL.
	DroppedValues []string
}

// ErrChecksApplied is returned when check runs for the epoch are already
// stored.
var ErrChecksApplied = fmt.Errorf("check runs already stored for the epoch")

// openFinding is the part of a stored finding the diff needs.
type openFinding struct {
	id, code, severity, state, detail string
	value                             sql.NullFloat64
	cause                             sql.NullInt64
	entityType, entityKey             string
}

// ApplyCheckRuns stores the check runs of one epoch and updates the
// findings in one transaction: a new failing entity starts firing, a
// severity change is recorded as changed, an entity the check no longer
// returns resolves. A check that failed to run leaves its findings as they
// are; a disabled check resolves its findings. runs must hold every check:
// the firing findings of a code without a run resolve, as that check no
// longer exists. Transitions carry the
// epoch's start time. The flapping flag is recomputed for every finding
// with a transition in the hour before the epoch.
func (s *Store) ApplyCheckRuns(ctx context.Context, epoch int64, runs []CheckRun) (ApplyResult, error) {
	var res ApplyResult
	if err := s.lock(ctx); err != nil {
		return res, err
	}
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return res, err
	}
	defer func() { _ = tx.Rollback() }()

	var at time.Time
	if err := tx.QueryRowContext(ctx, `SELECT started_at FROM epochs WHERE epoch = ?`, epoch).Scan(&at); err != nil {
		return res, fmt.Errorf("epoch %d: %w", epoch, err)
	}
	var done int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM check_runs WHERE epoch = ?`, epoch).Scan(&done); err != nil {
		return res, err
	}
	if done > 0 {
		return res, fmt.Errorf("epoch %d: %w", epoch, ErrChecksApplied)
	}
	open, err := loadFiringFindings(ctx, tx)
	if err != nil {
		return res, err
	}
	a := &applier{ctx: ctx, tx: tx, epoch: epoch, at: at.UTC(), open: open}
	ran := map[string]bool{}
	for _, r := range runs {
		ran[r.Code] = true
		a.run(r)
	}
	// A firing finding of a check that no longer exists (removed or
	// renamed in a newer binary) would otherwise fire for ever.
	var gone []string
	for code := range open {
		if !ran[code] {
			gone = append(gone, code)
		}
	}
	sort.Strings(gone)
	for _, code := range gone {
		for _, id := range sortedIDs(open[code]) {
			a.resolve(open[code][id])
		}
	}
	a.flush()
	a.flapping()
	if a.err != nil {
		return res, fmt.Errorf("apply checks for epoch %d: %w", epoch, a.err)
	}
	if err := tx.Commit(); err != nil {
		return res, err
	}
	sort.Strings(a.res.DroppedValues)
	return a.res, nil
}

// loadFiringFindings returns the firing findings by check code and ID.
func loadFiringFindings(ctx context.Context, tx *sql.Tx) (map[string]map[string]openFinding, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, check_code, severity, state, detail, value, cause_event_id, entity_type, entity_key
FROM findings WHERE state = 'firing'`)
	if err != nil {
		return nil, fmt.Errorf("read findings: %w", err)
	}
	defer rows.Close()
	out := map[string]map[string]openFinding{}
	for rows.Next() {
		var f openFinding
		if err := rows.Scan(&f.id, &f.code, &f.severity, &f.state, &f.detail, &f.value, &f.cause, &f.entityType, &f.entityKey); err != nil {
			return nil, err
		}
		if out[f.code] == nil {
			out[f.code] = map[string]openFinding{}
		}
		out[f.code][f.id] = f
	}
	return out, rows.Err()
}

// applier holds one ApplyCheckRuns transaction; the first error is kept
// and later calls do nothing.
type applier struct {
	ctx   context.Context
	tx    *sql.Tx
	epoch int64
	at    time.Time
	open  map[string]map[string]openFinding
	res   ApplyResult
	err   error

	// The rows of the epoch, written by flush with one statement per table
	// and batch: one statement per row took 9 seconds for 3000 new
	// findings, one parameter per value still 0.6 seconds.
	runs        []jsonRun
	inserts     []jsonFinding
	updates     []jsonFinding
	resolves    []string
	transitions []jsonTransition
	events      []jsonEvent
}

// jsonRun, jsonFinding, jsonTransition and jsonEvent are rows handed to
// DuckDB as one JSON array per statement and unnested there.
type jsonRun struct {
	Code     string  `json:"code"`
	Status   string  `json:"status"`
	Rows     int     `json:"rows"`
	Duration int64   `json:"duration_ms"`
	Error    *string `json:"error"`
}

type jsonFinding struct {
	ID         string   `json:"id"`
	Code       string   `json:"check_code"`
	EntityType string   `json:"entity_type"`
	EntityKey  string   `json:"entity_key"`
	Changed    bool     `json:"changed"`
	Severity   string   `json:"severity"`
	Detail     string   `json:"detail"`
	Value      *float64 `json:"value"`
	Cause      *int64   `json:"cause_event_id"`
	Related    *string  `json:"related_config"`
	Source     *string  `json:"related_source"`
	Began      *string  `json:"condition_since"`
}

type jsonTransition struct {
	ID         string   `json:"finding_id"`
	Code       string   `json:"check_code"`
	EntityType string   `json:"entity_type"`
	EntityKey  string   `json:"entity_key"`
	Kind       string   `json:"kind"`
	Severity   string   `json:"severity"`
	Detail     string   `json:"detail"`
	Value      *float64 `json:"value"`
	Cause      *int64   `json:"cause_event_id"`
}

type jsonEvent struct {
	Kind     string  `json:"kind"`
	Server   *string `json:"server_name"`
	Account  *string `json:"account"`
	Stream   *string `json:"stream"`
	Consumer *string `json:"consumer"`
	Client   *string `json:"client_name"`
	Summary  string  `json:"summary"`
	Raw      string  `json:"raw"`
}

// The from_json structures of the row types.
const (
	runShape        = `[{"code":"VARCHAR","status":"VARCHAR","rows":"INTEGER","duration_ms":"BIGINT","error":"VARCHAR"}]`
	findingShape    = `[{"id":"VARCHAR","check_code":"VARCHAR","entity_type":"VARCHAR","entity_key":"VARCHAR","changed":"BOOLEAN","severity":"VARCHAR","detail":"VARCHAR","value":"DOUBLE","cause_event_id":"BIGINT","related_config":"VARCHAR","related_source":"VARCHAR","condition_since":"TIMESTAMP"}]`
	transitionShape = `[{"finding_id":"VARCHAR","check_code":"VARCHAR","entity_type":"VARCHAR","entity_key":"VARCHAR","kind":"VARCHAR","severity":"VARCHAR","detail":"VARCHAR","value":"DOUBLE","cause_event_id":"BIGINT"}]`
	eventShape      = `[{"kind":"VARCHAR","server_name":"VARCHAR","account":"VARCHAR","stream":"VARCHAR","consumer":"VARCHAR","client_name":"VARCHAR","summary":"VARCHAR","raw":"VARCHAR"}]`
)

func (a *applier) exec(q string, args ...any) {
	if a.err != nil {
		return
	}
	if _, err := a.tx.ExecContext(a.ctx, q, args...); err != nil {
		a.err = fmt.Errorf("%s: %w", firstLine(q), err)
	}
}

// FindingID returns the ID of the finding of check code for an entity key.
func FindingID(code, entityKey string) string { return code + ":" + entityKey }

func (a *applier) run(r CheckRun) {
	n := 0
	if r.Status == RunOK {
		n = len(r.Rows)
	}
	run := jsonRun{Code: r.Code, Status: r.Status, Rows: n, Duration: r.Duration.Milliseconds()}
	if r.Error != "" {
		run.Error = &r.Error
	}
	a.runs = append(a.runs, run)
	switch r.Status {
	case RunOK:
		seen := map[string]bool{}
		// Rows are applied in key order, so that the transitions of one
		// epoch are written in a stable order.
		rows := append([]FindingRow(nil), r.Rows...)
		sort.Slice(rows, func(i, j int) bool { return rows[i].EntityKey < rows[j].EntityKey })
		for _, row := range rows {
			id := FindingID(r.Code, row.EntityKey)
			if seen[id] {
				continue
			}
			seen[id] = true
			a.fire(r, id, row)
		}
		for _, id := range sortedIDs(a.open[r.Code]) {
			if !seen[id] {
				a.resolve(a.open[r.Code][id])
			}
		}
	case RunDisabled:
		for _, id := range sortedIDs(a.open[r.Code]) {
			a.resolve(a.open[r.Code][id])
		}
	}
}

func sortedIDs(m map[string]openFinding) []string {
	out := make([]string, 0, len(m))
	for id := range m {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func relatedJSON(m map[string]string) any {
	if len(m) == 0 {
		return nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	return string(b)
}

// fire records a failing entity: a new or resolved finding starts firing,
// a firing one with another severity changes, and any firing one gets the
// latest detail, value and cause.
func (a *applier) fire(r CheckRun, id string, row FindingRow) {
	prev, firing := a.open[r.Code][id]
	value := row.Value
	if value != nil && (math.IsNaN(*value) || math.IsInf(*value, 0)) {
		// JSON and the API cannot carry it; the check's detail still can.
		value = nil
		a.res.DroppedValues = append(a.res.DroppedValues, id)
	}
	f := jsonFinding{ID: id, Code: r.Code, EntityType: r.EntityType, EntityKey: row.EntityKey, Severity: row.Severity,
		Detail: row.Detail, Value: value, Cause: idPtr(row.CauseEventID), Related: strPtr(relatedJSON(row.RelatedConfig)),
		Source: strPtr(nullIfEmpty(row.RelatedSource)), Began: timePtr(row.ConditionSince)}
	switch {
	case !firing:
		a.inserts = append(a.inserts, f)
		a.transition(f, TransitionFiring)
		a.res.Firing++
	case prev.severity != row.Severity:
		f.Changed = true
		a.updates = append(a.updates, f)
		a.transition(f, TransitionChanged)
		a.res.Changed++
	default:
		a.updates = append(a.updates, f)
	}
}

func idPtr(id int64) *int64 {
	if id <= 0 {
		return nil
	}
	return &id
}

// strPtr turns a nullable value (nil or a string) into a JSON field.
func strPtr(v any) *string {
	if s, ok := v.(string); ok {
		return &s
	}
	return nil
}

// resolve records that a firing finding is no longer returned.
func (a *applier) resolve(f openFinding) {
	a.resolves = append(a.resolves, f.id)
	jf := jsonFinding{ID: f.id, Code: f.code, EntityType: f.entityType, EntityKey: f.entityKey, Severity: f.severity, Detail: f.detail}
	if f.value.Valid {
		jf.Value = &f.value.Float64
	}
	if f.cause.Valid {
		jf.Cause = &f.cause.Int64
	}
	a.transition(jf, TransitionResolved)
	a.res.Resolved++
}

func (a *applier) transition(f jsonFinding, kind string) {
	a.transitions = append(a.transitions, jsonTransition{ID: f.ID, Code: f.Code, EntityType: f.EntityType, EntityKey: f.EntityKey,
		Kind: kind, Severity: f.Severity, Detail: f.Detail, Value: f.Value, Cause: f.Cause})
	a.findingEvent(f, kind)
}

// batchRows is the most rows one statement carries.
const batchRows = 5000

// insertJSON runs query once per batch of rows, with the batch as its one
// parameter: a JSON array the query unnests with from_json and shape.
func insertJSON[T any](a *applier, query string, rows []T) {
	for len(rows) > 0 && a.err == nil {
		n := min(len(rows), batchRows)
		b, err := json.Marshal(rows[:n])
		if err != nil {
			a.err = err
			return
		}
		a.exec(query, string(b))
		rows = rows[n:]
	}
}

// fromJSON is the FROM clause that unnests a batch of shape.
func fromJSON(shape string) string {
	return `FROM (SELECT unnest(from_json(?::VARCHAR, '` + shape + `'), recursive := true))`
}

// flush writes the collected rows, each table with one statement per
// batch. Times and the epoch are literals: DuckDB cannot bind parameters
// in an UPDATE ... FROM.
func (a *applier) flush() {
	at, epoch := tsMicroLiteral(a.at), a.epoch
	insertJSON(a, fmt.Sprintf(`INSERT INTO check_runs (epoch, check_code, status, rows, duration_ms, error)
SELECT %d, code, status, rows, duration_ms, error `+fromJSON(runShape), epoch), a.runs)
	insertJSON(a, fmt.Sprintf(`INSERT INTO findings (id, check_code, entity_type, entity_key, severity, state, last_transition,
    since, since_epoch, transitioned_at, resolved_at, last_seen_epoch, updated_epoch, detail, value,
    cause_event_id, related_config, related_source, transitions_last_hour, flapping, condition_since)
SELECT id, check_code, entity_type, entity_key, severity, 'firing', 'firing', %[1]s, %[2]d, %[1]s, NULL, %[2]d, %[2]d,
    detail, value, cause_event_id, related_config::JSON, related_source, 0, false, condition_since `+fromJSON(findingShape)+`
ON CONFLICT (id) DO UPDATE SET severity = excluded.severity, state = 'firing', last_transition = 'firing',
    since = excluded.since, since_epoch = excluded.since_epoch, transitioned_at = excluded.transitioned_at,
    resolved_at = NULL, last_seen_epoch = excluded.last_seen_epoch, updated_epoch = excluded.updated_epoch,
    detail = excluded.detail, value = excluded.value, cause_event_id = excluded.cause_event_id,
    related_config = excluded.related_config, related_source = excluded.related_source,
    condition_since = excluded.condition_since`, at, epoch), a.inserts)
	if len(a.updates) > 0 {
		a.exec(`CREATE OR REPLACE TEMP TABLE apply_updates (id VARCHAR, changed BOOLEAN, severity VARCHAR, detail VARCHAR,
    value DOUBLE, cause_event_id BIGINT, related_config JSON, related_source VARCHAR, condition_since TIMESTAMP)`)
		insertJSON(a, `INSERT INTO apply_updates SELECT id, changed, severity, detail, value, cause_event_id, related_config::JSON,
    related_source, condition_since `+fromJSON(findingShape), a.updates)
		a.exec(fmt.Sprintf(`UPDATE findings SET severity = u.severity,
    last_transition = CASE WHEN u.changed THEN 'changed' ELSE findings.last_transition END,
    transitioned_at = CASE WHEN u.changed THEN %[1]s ELSE findings.transitioned_at END,
    last_seen_epoch = %[2]d, updated_epoch = %[2]d, detail = u.detail, value = u.value, cause_event_id = u.cause_event_id,
    related_config = u.related_config, related_source = u.related_source, condition_since = u.condition_since
FROM apply_updates u WHERE findings.id = u.id`, at, epoch))
		a.exec(`DROP TABLE apply_updates`)
	}
	if len(a.resolves) > 0 {
		a.exec(`CREATE OR REPLACE TEMP TABLE apply_resolves (id VARCHAR)`)
		insertJSON(a, `INSERT INTO apply_resolves SELECT unnest(from_json(?::VARCHAR, '["VARCHAR"]'))`, a.resolves)
		a.exec(fmt.Sprintf(`UPDATE findings SET state = 'resolved', last_transition = 'resolved', transitioned_at = %[1]s,
    resolved_at = %[1]s, updated_epoch = %[2]d WHERE id IN (SELECT id FROM apply_resolves)`, at, epoch))
		a.exec(`DROP TABLE apply_resolves`)
	}
	insertJSON(a, fmt.Sprintf(`INSERT INTO finding_transitions (finding_id, check_code, entity_type, entity_key, epoch, time, kind,
    severity, detail, value, cause_event_id)
SELECT finding_id, check_code, entity_type, entity_key, %[2]d, %[1]s, kind, severity, detail, value, cause_event_id `+
		fromJSON(transitionShape), at, epoch), a.transitions)
	insertJSON(a, fmt.Sprintf(`INSERT INTO events (id, time, received_at, source, type, kind, server_name, account, stream, consumer,
    client_name, summary, raw)
SELECT nextval('event_seq'), %[1]s, %[1]s, '%[2]s', NULL, kind, server_name, account, stream, consumer, client_name, summary, raw::JSON `+
		fromJSON(eventShape), at, SourceFinding), a.events)
}

// SourceFinding is the events.source of a finding's transition.
const SourceFinding = "finding"

// findingEvent writes the transition as an event in the same transaction,
// so that the event list and the timeline's lane show it with the NATS
// events. The event's time is the epoch's start, like the transition's.
func (a *applier) findingEvent(f jsonFinding, kind string) {
	ev := jsonEvent{Kind: "finding." + kind, Summary: f.Code + " " + kind + ": " + f.Detail}
	key := f.EntityKey
	parts := strings.Split(key, "/")
	switch {
	case f.EntityType == "server":
		ev.Server = &key
	case f.EntityType == "client":
		ev.Client = &key
	case f.EntityType == "account":
		ev.Account = &key
	case f.EntityType == "stream" && len(parts) == 2:
		ev.Account, ev.Stream = &parts[0], &parts[1]
	case f.EntityType == "consumer" && len(parts) == 3:
		ev.Account, ev.Stream, ev.Consumer = &parts[0], &parts[1], &parts[2]
	}
	raw, err := json.Marshal(map[string]any{
		"finding_id": f.ID, "check_code": f.Code, "entity_type": f.EntityType, "entity_key": key, "transition": kind,
		"severity": f.Severity, "detail": f.Detail, "value": f.Value, "cause_event_id": f.Cause, "epoch": a.epoch,
	})
	if err != nil {
		if a.err == nil {
			a.err = fmt.Errorf("finding event %s: %w", f.ID, err)
		}
		return
	}
	ev.Raw = string(raw)
	a.events = append(a.events, ev)
}

// flapping recounts the transitions in the hour up to the epoch for every
// finding that had one in that hour or was counted before, so that the
// count also falls once transitions age out.
func (a *applier) flapping() {
	// The values are formatted into the statement: DuckDB cannot bind
	// parameters in this UPDATE ... FROM.
	from, to := tsLiteral(a.at.Add(-FlappingWindow)), tsLiteral(a.at)
	a.exec(fmt.Sprintf(`UPDATE findings SET transitions_last_hour = c.n, flapping = c.cycles > %[1]d, updated_epoch = %[2]d
FROM (
    SELECT f.id, count(t.finding_id) AS n, count(t.finding_id) FILTER (WHERE t.kind = 'resolved') AS cycles
    FROM findings f
    LEFT JOIN finding_transitions t ON t.finding_id = f.id AND t.time > %[3]s AND t.time <= %[4]s
    WHERE f.transitions_last_hour > 0 OR f.id IN (
        SELECT finding_id FROM finding_transitions WHERE time > %[3]s AND time <= %[4]s)
    GROUP BY f.id
) c
WHERE findings.id = c.id AND (findings.transitions_last_hour <> c.n OR findings.flapping <> (c.cycles > %[1]d))`, FlappingAbove, a.epoch, from, to))
}

// tsMicroLiteral is a TIMESTAMP literal with microseconds, as DuckDB
// stores them; tsLiteral's whole seconds would move a transition's time.
func tsMicroLiteral(t time.Time) string {
	return "TIMESTAMP '" + t.UTC().Format("2006-01-02 15:04:05.000000") + "'"
}

// timePtr formats an optional time for a from_json TIMESTAMP field.
func timePtr(t *time.Time) *string {
	if t == nil || t.IsZero() {
		return nil
	}
	v := t.UTC().Format("2006-01-02 15:04:05.999999")
	return &v
}
