//go:build !(js && wasm)

// The engine reads and writes the DuckDB store, which needs cgo and so
// does not build for WebAssembly. The tag leaves it out of the browser demo
// (cmd/natslens-wasm), which needs only the check definitions; CI builds
// that command, so a new store import elsewhere in the package fails there.

package checks

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shadone/nats-lens/internal/store"
)

// Engine runs a Set of checks for one epoch.
type Engine struct {
	// Interval is the configured collect interval; unknown means no slow routes.
	Interval time.Duration
	Set      *Set
	// Log receives failed checks and the run summary; nil uses
	// slog.Default().
	Log *slog.Logger
	// Budget bounds the time all checks of one epoch may take; a check
	// that would start after it is recorded as an error without running.
	// Zero means no bound beyond each check's timeout.
	Budget time.Duration
	// ToolingNames are the nats-lens connection names of the engine's
	// system, which the client checks leave out; nil means the Set's.
	// Engines of several systems share one Set and each sets its own.
	ToolingNames []string
	// ArchiveSince is when the process began exporting hours to the
	// archive, read by the checks as archive_since; zero when it does
	// not export.
	ArchiveSince time.Time
	// only, when set, limits a run to these codes; for tests.
	only map[string]bool
	// loopbackWarned is set once the loopback guard was logged; the
	// engine's, so that engines sharing a Set neither race on it nor
	// hide each other's warning.
	loopbackWarned bool
}

// tooling is the engine's tooling names, or the Set's without its own.
func (e *Engine) tooling() []string {
	if e.ToolingNames != nil {
		return e.ToolingNames
	}
	return e.Set.tooling
}

func (e *Engine) logger() *slog.Logger {
	if e.Log == nil {
		return slog.Default()
	}
	return e.Log
}

// Run evaluates every check for the epoch and stores the outcome. A check
// that fails becomes a run with status error and a system-001 finding; only
// a store failure or ctx ending is returned as an error, and then nothing
// is stored.
func (e *Engine) Run(ctx context.Context, st *store.Store, epoch int64) (store.ApplyResult, error) {
	runs, loopback, err := e.evaluate(ctx, st.DB(), epoch)
	if err != nil {
		return store.ApplyResult{}, err
	}
	// The loopback decision is written before and apart from the check
	// runs' transaction. That is safe: it depends only on the epoch's and
	// the trailing epochs' connections, not on the runs, and the write
	// sets it only while it is NULL, so a retried epoch (ApplyCheckRuns
	// failing, or ErrChecksApplied) keeps the decision it first got.
	if err := st.SetLoopbackTooling(ctx, epoch, loopback); err != nil {
		return store.ApplyResult{}, err
	}
	res, err := st.ApplyCheckRuns(ctx, epoch, runs)
	if err != nil {
		return res, err
	}
	if len(res.DroppedValues) > 0 {
		e.logger().Warn("check values not stored: not a finite number", "epoch", epoch, "findings", res.DroppedValues)
	}
	failed := 0
	for _, r := range runs {
		if r.Status == store.RunError {
			failed++
		}
	}
	e.logger().Debug("checks applied", "epoch", epoch, "checks", len(runs), "failed", failed,
		"firing", res.Firing, "changed", res.Changed, "resolved", res.Resolved)
	return res, nil
}

// Evaluate runs every check against db for the epoch and returns the runs
// in code order, the engine's own system-001 included. It writes nothing.
// It returns an error only when the epoch is not stored or ctx ends; a
// failing check is a run with status error.
func (e *Engine) Evaluate(ctx context.Context, db *sql.DB, epoch int64) ([]store.CheckRun, error) {
	runs, _, err := e.evaluate(ctx, db, epoch)
	return runs, err
}

// evaluate is Evaluate that also returns the epoch's loopback decision,
// which Run stores with the epoch.
func (e *Engine) evaluate(ctx context.Context, db *sql.DB, epoch int64) ([]store.CheckRun, bool, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, false, err
	}
	defer conn.Close()
	var at time.Time
	var prev int64
	err = conn.QueryRowContext(ctx, `SELECT started_at, (SELECT coalesce(max(epoch), 0) FROM epochs WHERE epoch < ?)
FROM epochs WHERE epoch = ?`, epoch, epoch).Scan(&at, &prev)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, fmt.Errorf("epoch %d is not stored", epoch)
	}
	if err != nil {
		return nil, false, fmt.Errorf("read epoch %d: %w", epoch, err)
	}
	at = at.UTC()
	tooling := e.tooling()
	archiveSince := "NULL::TIMESTAMP"
	if !e.ArchiveSince.IsZero() {
		archiveSince = tsLit(e.ArchiveSince)
	}
	loopback, _, err := LoopbackDecision(ctx, conn, epoch, tooling)
	if err != nil {
		return nil, false, err
	}
	if !loopback && e.Set.loopbackTooling && !e.loopbackWarned {
		e.loopbackWarned = true
		e.logger().Warn("most client connections come from a loopback address; they are not treated as tooling",
			"epoch", epoch)
	}
	if _, err := conn.ExecContext(ctx, Prelude); err != nil {
		return nil, false, fmt.Errorf("define macros: %w", err)
	}
	if err := setVars(ctx, conn, map[string]string{
		"epoch": strconv.FormatInt(epoch, 10) + "::BIGINT", "epoch_time": tsLit(at), "prev_epoch": strconv.FormatInt(prev, 10) + "::BIGINT",
		"restart_window":   Value{Unit: UnitDuration, Duration: e.Set.RestartWindow()}.literal(),
		"exclude_tooling":  strconv.FormatBool(!e.Set.includeTooling),
		"tooling_names":    listLiteral(tooling),
		"loopback_setting": strconv.FormatBool(e.Set.loopbackTooling),
		"loopback_now":     strconv.FormatBool(loopback),
		"archive_since":    archiveSince,
	}); err != nil {
		return nil, false, err
	}
	var runs []store.CheckRun
	var failed []string
	var deadline time.Time
	if e.Budget > 0 {
		deadline = time.Now().Add(e.Budget)
	}
	var skipped []string
	for _, c := range rotate(e.Set.Checks(), epoch) {
		if c.Code == CheckFailedCode || (e.only != nil && !e.only[c.Code]) {
			continue
		}
		run := store.CheckRun{Code: c.Code, EntityType: c.Entity}
		if e.Set.Disabled(c.Code) {
			run.Status = store.RunDisabled
			runs = append(runs, run)
			continue
		}
		start := time.Now()
		if !deadline.IsZero() && deadline.Sub(start) < minQueryTime {
			run.Status, run.Error = store.RunError, fmt.Sprintf("not run: the checks of this collection used up their %s", e.Budget)
			failed = append(failed, fmt.Sprintf("%s (%s)", c.Code, run.Error))
			skipped = append(skipped, c.Code)
			runs = append(runs, run)
			continue
		}
		rows, err := e.runOne(ctx, conn, c, epoch, at, deadline)
		run.Duration = time.Since(start)
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		if err != nil {
			run.Status, run.Error = store.RunError, err.Error()
			failed = append(failed, fmt.Sprintf("%s (%s)", c.Code, err.Error()))
			e.logger().Warn("check failed", "check", c.Code, "epoch", epoch, "err", err)
		} else {
			run.Status, run.Rows = store.RunOK, rows
		}
		runs = append(runs, run)
	}
	runs = e.suppress(runs)
	if len(skipped) > 0 {
		sort.Strings(skipped)
		e.logger().Warn("checks not run: the epoch's budget was used up", "epoch", epoch, "budget", e.Budget, "checks", skipped)
	}
	streak, err := failingStreak(ctx, conn, epoch, runs)
	if err != nil {
		return nil, false, err
	}
	runs = append(runs, e.failedRun(failed, streak))
	sort.Slice(runs, func(i, j int) bool { return runs[i].Code < runs[j].Code })
	return runs, loopback, nil
}

// suppress drops from each check's rows the entities that a check named
// in its suppressed_by returned in the same epoch.
func (e *Engine) suppress(runs []store.CheckRun) []store.CheckRun {
	keys := map[string]map[string]bool{}
	for _, r := range runs {
		if r.Status != store.RunOK {
			continue
		}
		keys[r.Code] = map[string]bool{}
		for _, row := range r.Rows {
			keys[r.Code][row.EntityKey] = true
		}
	}
	out := make([]store.CheckRun, len(runs))
	for i, r := range runs {
		out[i] = r
		c, ok := e.Set.Lookup(r.Code)
		if !ok || len(c.SuppressedBy) == 0 || r.Status != store.RunOK {
			continue
		}
		var rows []store.FindingRow
		for _, row := range r.Rows {
			hidden := false
			for _, code := range c.SuppressedBy {
				hidden = hidden || keys[code][row.EntityKey]
			}
			if !hidden {
				rows = append(rows, row)
			}
		}
		out[i].Rows = rows
	}
	return out
}

// minQueryTime is the least time left in the budget for a check to start:
// with less, it is recorded as not run instead of as timed out after a few
// milliseconds.
const minQueryTime = 50 * time.Millisecond

// rotate returns the checks starting at a position that moves by one each
// epoch, so that when the budget runs out it is not always the same checks
// that are left over.
func rotate(cs []*Check, epoch int64) []*Check {
	if len(cs) == 0 {
		return cs
	}
	k := int(epoch % int64(len(cs)))
	return append(append([]*Check{}, cs[k:]...), cs[:k]...)
}

// failingStreak returns the checks that failed in this epoch and in each of
// the EscalateAfter-1 stored epochs before it.
func failingStreak(ctx context.Context, conn *sql.Conn, epoch int64, runs []store.CheckRun) ([]string, error) {
	var now []string
	for _, r := range runs {
		if r.Status == store.RunError {
			now = append(now, r.Code)
		}
	}
	if len(now) == 0 {
		return nil, nil
	}
	rows, err := conn.QueryContext(ctx, fmt.Sprintf(`
WITH prev AS (SELECT epoch FROM epochs WHERE epoch < %[1]d ORDER BY epoch DESC LIMIT %[2]d)
SELECT check_code FROM check_runs
WHERE epoch IN (SELECT epoch FROM prev) AND status = 'error'
GROUP BY check_code
HAVING count(*) = %[2]d AND (SELECT count(*) FROM prev) = %[2]d`, epoch, EscalateAfter-1))
	if err != nil {
		return nil, fmt.Errorf("read earlier check runs: %w", err)
	}
	defer rows.Close()
	before := map[string]bool{}
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, err
		}
		before[code] = true
	}
	var out []string
	for _, c := range now {
		if before[c] {
			out = append(out, c)
		}
	}
	return out, rows.Err()
}

// failedRun is the run of system-001: one finding for the system while any
// check failed in the epoch, critical when one has failed EscalateAfter
// epochs in a row (streak).
func (e *Engine) failedRun(failed, streak []string) store.CheckRun {
	c, _ := e.Set.Lookup(CheckFailedCode)
	run := store.CheckRun{Code: c.Code, EntityType: c.Entity, Status: store.RunOK}
	if e.Set.Disabled(c.Code) {
		run.Status = store.RunDisabled
		return run
	}
	if len(failed) > 0 {
		n := float64(len(failed))
		detail := fmt.Sprintf("%d check failed to run: %s.", len(failed), strings.Join(failed, "; "))
		if len(failed) > 1 {
			detail = fmt.Sprintf("%d checks failed to run: %s.", len(failed), strings.Join(failed, "; "))
		}
		severity := c.Severity
		if len(streak) > 0 {
			severity = "critical"
			detail += escalationText(streak)
		}
		run.Rows = []store.FindingRow{{EntityKey: "", Severity: severity, Detail: detail, Value: &n}}
	}
	return run
}

// runOne sets the check's variables and runs its query or Go rule within
// the set's timeout and the remaining epoch budget.
func (e *Engine) runOne(ctx context.Context, conn *sql.Conn, c *Check, epoch int64, at, deadline time.Time) ([]store.FindingRow, error) {
	vars := map[string]string{}
	if lb := e.Set.Lookback(c.Code); lb > 0 {
		vars["lookback"] = Value{Unit: UnitDuration, Duration: lb}.literal()
		vars["lookback_text"] = "'" + DurationText(lb) + "'"
		vars["window_start"] = tsLit(at.Add(-lb))
		vars["window_epoch"] = fmt.Sprintf("(SELECT coalesce(min(epoch), getvariable('epoch')) FROM epochs WHERE started_at >= %s)", tsLit(at.Add(-lb)))
	}
	for _, t := range c.Thresholds {
		v, _ := e.Set.Value(c.Code, t.Name)
		vars[t.Name] = v.literal()
	}
	if err := setVars(ctx, conn, vars); err != nil {
		return nil, err
	}
	timeout := e.Set.Timeout()
	if !deadline.IsZero() {
		timeout = min(timeout, time.Until(deadline))
	}
	qctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if c.Code == LeafHealthCode {
		return declared(c, e.logger())(e.leafHealth(qctx, conn, epoch, at))
	}
	if c.Code == SlowRouteCode {
		return declared(c, e.logger())(e.slowRoutes(qctx, conn, epoch, at))
	}
	if c.Code == "system-002" || c.Code == "system-003" {
		lo := epoch
		if c.Code == "system-003" {
			if err := conn.QueryRowContext(qctx, "SELECT getvariable('window_epoch')").Scan(&lo); err != nil {
				return nil, err
			}
		}
		if err := prepareMetaGroups(qctx, conn, lo, epoch); err != nil {
			return nil, err
		}
	}
	rows, err := conn.QueryContext(qctx, c.SQL)
	if err != nil {
		if qctx.Err() != nil && ctx.Err() == nil {
			return nil, fmt.Errorf("no result within %s", timeout.Round(time.Millisecond))
		}
		return nil, err
	}
	defer rows.Close()
	out, err := scanRows(rows, c)
	if err != nil {
		if qctx.Err() != nil && ctx.Err() == nil {
			return nil, fmt.Errorf("no result within %s", timeout.Round(time.Millisecond))
		}
		return nil, err
	}
	return out, nil
}

// setVars sets DuckDB session variables on conn; values are SQL literals
// or constant expressions.
func setVars(ctx context.Context, conn *sql.Conn, vars map[string]string) error {
	names := make([]string, 0, len(vars))
	for n := range vars {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if _, err := conn.ExecContext(ctx, "SET VARIABLE "+n+" = "+vars[n]); err != nil {
			return fmt.Errorf("set variable %s: %w", n, err)
		}
	}
	return nil
}

func tsLit(t time.Time) string {
	return "TIMESTAMP '" + t.UTC().Format("2006-01-02 15:04:05.999999") + "'"
}

// resultColumns are the columns a check may return; the first three are
// required.
var resultColumns = map[string]bool{
	"entity_key": true, "severity": true, "detail": true,
	"value": true, "cause_event_id": true, "related_config": true, "related_source": true, "condition_since": true,
}

// scanRows reads a check's result into finding rows and checks every
// column, key and severity.
func scanRows(rows *sql.Rows, c *Check) ([]store.FindingRow, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	idx := map[string]int{}
	for i, col := range cols {
		if !resultColumns[col] {
			return nil, fmt.Errorf("unexpected result column %q", col)
		}
		idx[col] = i
	}
	for _, col := range []string{"entity_key", "detail"} {
		if _, ok := idx[col]; !ok {
			return nil, fmt.Errorf("result has no %s column", col)
		}
	}
	var out []store.FindingRow
	seen := map[string]bool{}
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		get := func(col string) any {
			if i, ok := idx[col]; ok {
				return vals[i]
			}
			return nil
		}
		key := text(get("entity_key"))
		if err := checkKey(c.Entity, key, get("entity_key") == nil); err != nil {
			return nil, err
		}
		if seen[key] {
			return nil, fmt.Errorf("entity %q returned twice", key)
		}
		seen[key] = true
		r := store.FindingRow{EntityKey: key, Severity: c.Severity, Detail: text(get("detail"))}
		if s := get("severity"); s != nil {
			r.Severity = text(s)
			if !severities[r.Severity] {
				return nil, fmt.Errorf("entity %q: severity %q is not info, warning or critical", key, r.Severity)
			}
		}
		if r.Detail == "" {
			r.Detail = c.Name
		}
		if v := get("value"); v != nil {
			f, err := number(v)
			if err != nil {
				return nil, fmt.Errorf("entity %q: value: %w", key, err)
			}
			r.Value = &f
		}
		if v := get("cause_event_id"); v != nil {
			f, err := number(v)
			if err != nil {
				return nil, fmt.Errorf("entity %q: cause_event_id: %w", key, err)
			}
			r.CauseEventID = int64(f)
		}
		if v := get("related_config"); v != nil {
			m, err := relatedConfig(v)
			if err != nil {
				return nil, fmt.Errorf("entity %q: related_config: %w", key, err)
			}
			r.RelatedConfig = m
		}
		r.RelatedSource = text(get("related_source"))
		if v := get("condition_since"); v != nil {
			t, ok := v.(time.Time)
			if !ok {
				return nil, fmt.Errorf("entity %q: condition_since: %T is not a timestamp", key, v)
			}
			t = t.UTC()
			r.ConditionSince = &t
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// checkKey checks that key has the shape of the entity type's keys.
func checkKey(entity, key string, null bool) error {
	parts := entityTypes[entity]
	switch {
	case null:
		return fmt.Errorf("entity_key is null")
	case parts < 0:
		if key != "" {
			return fmt.Errorf("a system finding has an empty entity_key, got %q", key)
		}
		return nil
	case key == "":
		return fmt.Errorf("entity_key is empty")
	case parts == 0:
		return nil
	}
	split := strings.Split(key, "/")
	if len(split) != parts {
		return fmt.Errorf("a %s key has %d parts separated by /, got %q", entity, parts, key)
	}
	for _, p := range split {
		if p == "" {
			return fmt.Errorf("a %s key has no empty parts, got %q", entity, key)
		}
	}
	return nil
}

func text(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case []byte:
		return string(x)
	}
	return fmt.Sprint(v)
}

func number(v any) (float64, error) {
	switch x := v.(type) {
	case float64:
		return x, nil
	case float32:
		return float64(x), nil
	case int64:
		return float64(x), nil
	case int32:
		return float64(x), nil
	case int16:
		return float64(x), nil
	case int8:
		return float64(x), nil
	case int:
		return float64(x), nil
	case uint64:
		return float64(x), nil
	case uint32:
		return float64(x), nil
	case interface{ Float64() float64 }:
		return x.Float64(), nil
	case string:
		return strconv.ParseFloat(x, 64)
	}
	return 0, fmt.Errorf("%v (%T) is not a number", v, v)
}

// relatedConfig reads a JSON object (as text or decoded) into setting
// name to value text.
func relatedConfig(v any) (map[string]string, error) {
	var m map[string]any
	switch x := v.(type) {
	case map[string]any:
		m = x
	case string, []byte:
		if err := json.Unmarshal([]byte(text(x)), &m); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("%T is not a JSON object", v)
	}
	out := map[string]string{}
	for k, val := range m {
		if val == nil {
			continue
		}
		switch t := val.(type) {
		case string:
			out[k] = t
		case float64:
			out[k] = strconv.FormatFloat(t, 'f', -1, 64)
		default:
			b, _ := json.Marshal(t)
			out[k] = string(b)
		}
	}
	return out, nil
}

// escalationText is the sentence system-001's detail gains when checks
// failed in each of the last EscalateAfter collections.
func escalationText(streak []string) string {
	return fmt.Sprintf(" Failing in each of the last %d collections: %s.", EscalateAfter, strings.Join(streak, ", "))
}

// declared passes a built-in check's rows on, and logs a warning for a
// row whose severity the check does not declare, which its severity rule
// then does not cover. The row is kept: a labelling slip must not cost the
// epoch its findings; TestBuiltinSeverityRules catches it in tests.
func declared(c *Check, log *slog.Logger) func([]store.FindingRow, error) ([]store.FindingRow, error) {
	return func(rows []store.FindingRow, err error) ([]store.FindingRow, error) {
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			if !slices.Contains(c.severities, r.Severity) {
				log.Warn("built-in check gave a severity it does not declare", "check", c.Code, "entity", r.EntityKey, "severity", r.Severity)
			}
		}
		return rows, nil
	}
}
