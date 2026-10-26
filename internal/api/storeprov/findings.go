package storeprov

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/checks"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
)

var severityOfText = map[string]v1.Severity{
	"info": v1.Severity_SEVERITY_INFO, "warning": v1.Severity_SEVERITY_WARNING, "critical": v1.Severity_SEVERITY_CRITICAL,
}

// stateOf maps a stored finding to its API state: a flapping finding is
// flapping, a firing one whose last transition changed its severity is
// changed.
func stateOf(f store.Finding) v1.FindingState {
	switch {
	case !f.Firing():
		return v1.FindingState_FINDING_STATE_RESOLVED
	case f.Flapping:
		return v1.FindingState_FINDING_STATE_FLAPPING
	case f.LastTransition == store.TransitionChanged:
		return v1.FindingState_FINDING_STATE_CHANGED
	}
	return v1.FindingState_FINDING_STATE_FIRING
}

var transitionState = map[string]v1.FindingState{
	store.TransitionFiring:   v1.FindingState_FINDING_STATE_FIRING,
	store.TransitionChanged:  v1.FindingState_FINDING_STATE_CHANGED,
	store.TransitionResolved: v1.FindingState_FINDING_STATE_RESOLVED,
}

// entityRef builds the API reference of a finding's entity.
func entityRef(typ, key string) *v1.EntityRef {
	t := entityTypeOf(typ)
	parts := strings.Split(key, "/")
	switch {
	case t == v1.EntityType_ENTITY_TYPE_SYSTEM:
		return &v1.EntityRef{Type: t}
	case (t == v1.EntityType_ENTITY_TYPE_STREAM && len(parts) == 2) || (t == v1.EntityType_ENTITY_TYPE_CONSUMER && len(parts) == 3) ||
		t == v1.EntityType_ENTITY_TYPE_SERVER:
		anys := make([]any, len(parts))
		for i, x := range parts {
			anys[i] = x
		}
		if t == v1.EntityType_ENTITY_TYPE_SERVER {
			anys = []any{key}
		}
		return refOf(t, anys)
	case t == v1.EntityType_ENTITY_TYPE_ACCOUNT:
		return &v1.EntityRef{Type: t, Key: key, Name: key, Account: key}
	}
	return &v1.EntityRef{Type: t, Key: key, Name: key}
}

// formatValue writes a check's number without float noise.
func formatValue(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// findingProto converts a stored finding; causes maps cause event IDs to
// their events.
func (p *Provider) findingProto(f store.Finding, causes map[int64]*v1.EventRef) *v1.Finding {
	out := &v1.Finding{
		Id: f.ID, CheckCode: f.CheckCode, Severity: severityOfText[f.Severity], State: stateOf(f),
		Entity: entityRef(f.EntityType, f.EntityKey), Since: ts(f.Since), TransitionedAt: ts(f.TransitionedAt),
		ResolvedAt: ts(f.ResolvedAt), Detail: f.Detail, RelatedConfigSource: f.RelatedSource, ConditionSince: tsp(f.ConditionSince),
		TransitionsLastHour: int32(f.TransitionsLastHour), Cause: causes[f.CauseEventID], Expiry: checks.IsExpiry(f.CheckCode),
	}
	var marked []string
	if p.Checks != nil {
		if c, ok := p.Checks.Lookup(f.CheckCode); ok {
			out.CheckName, out.Remediation, marked = c.Name, c.Remediation, c.RelatedConfig
			if f.Value != nil {
				out.Value = &v1.FindingValue{Name: c.ValueName, Basis: c.ValueBasis}
			}
		}
	}
	if f.Value != nil {
		if out.Value == nil {
			out.Value = &v1.FindingValue{}
		}
		out.Value.Number, out.Value.Display = ptr(*f.Value), formatValue(*f.Value)
	}
	keys := make([]string, 0, len(f.RelatedConfig))
	for k := range f.RelatedConfig {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		out.RelatedConfig = append(out.RelatedConfig, &v1.ConfigValue{Key: k, Value: f.RelatedConfig[k], Highlight: slices.Contains(marked, k)})
	}
	if f.Mute != nil {
		out.Mute = muteProto(*f.Mute)
	}
	return out
}

// eventRefs reads the events with the IDs.
func (p *Provider) eventRefs(ctx context.Context, ids []int64) (map[int64]*v1.EventRef, error) {
	out := map[int64]*v1.EventRef{}
	var want []string
	for _, id := range ids {
		if id > 0 {
			want = append(want, strconv.FormatInt(id, 10))
		}
	}
	if len(want) == 0 {
		return out, nil
	}
	slices.Sort(want)
	want = slices.Compact(want)
	rows, err := p.db().QueryContext(ctx, `SELECT `+eventCols+` FROM events e WHERE e.id IN (`+strings.Join(want, ", ")+`)`)
	if err != nil {
		return nil, fmt.Errorf("cause events: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		ev, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		id, _ := strconv.ParseInt(ev.GetId(), 10, 64)
		out[id] = &v1.EventRef{Id: ev.GetId(), Time: ev.GetTime(), Kind: ev.GetKind(), Summary: ev.GetSummary()}
	}
	return out, rows.Err()
}

// findingProtos converts findings with their cause events.
func (p *Provider) findingProtos(ctx context.Context, fs []store.Finding) ([]*v1.Finding, error) {
	ids := make([]int64, len(fs))
	for i, f := range fs {
		ids[i] = f.CauseEventID
	}
	causes, err := p.eventRefs(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]*v1.Finding, len(fs))
	for i, f := range fs {
		out[i] = p.findingProto(f, causes)
	}
	return out, nil
}

func severityRank(s v1.Severity) int {
	switch s {
	case v1.Severity_SEVERITY_CRITICAL:
		return 0
	case v1.Severity_SEVERITY_WARNING:
		return 1
	case v1.Severity_SEVERITY_INFO:
		return 2
	}
	return 3
}

// sortWorstFirst orders findings worst first by api.CompareWorst: an
// expiry finding after the other findings of its severity.
func sortWorstFirst(fs []*v1.Finding) {
	slices.SortStableFunc(fs, api.CompareWorst)
}

func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
}

// matchesEntity reports whether e is want or belongs to it: a stream
// covers its consumers. The system and an unset filter match everything.
func matchesEntity(e, want *v1.EntityRef) bool {
	switch want.GetType() {
	case v1.EntityType_ENTITY_TYPE_UNSPECIFIED, v1.EntityType_ENTITY_TYPE_SYSTEM:
		return true
	}
	if e.GetType() == want.GetType() && e.GetKey() == want.GetKey() {
		return true
	}
	return want.GetType() == v1.EntityType_ENTITY_TYPE_STREAM && e.GetType() == v1.EntityType_ENTITY_TYPE_CONSUMER &&
		strings.HasPrefix(e.GetKey(), want.GetKey()+"/")
}

// past reports whether a range ends more than one interval before now; its
// findings are rebuilt from the transitions as they stood at its end.
func (p *Provider) past(r *v1.TimeRange) bool {
	_, end := span(r)
	return end.Before(p.Now().Add(-max(p.Interval, time.Second)))
}

// findings reads the findings that fired at some time in the range, worst
// first. For a current range they are the findings as they are now, with
// the mutes active now; for a past range they are rebuilt as they stood at
// its end, with the mutes active then.
func (p *Provider) findings(ctx context.Context, r *v1.TimeRange) ([]*v1.Finding, error) {
	start, end := span(r)
	var fs []store.Finding
	var err error
	if p.past(r) {
		fs, err = p.St.FindingsAt(ctx, start, end)
	} else {
		fs, err = p.St.Findings(ctx, p.Now(), store.FindingQuery{From: start, To: end})
	}
	if err != nil {
		return nil, err
	}
	out, err := p.findingProtos(ctx, fs)
	if err != nil {
		return nil, err
	}
	sortWorstFirst(out)
	return out, nil
}

// firing keeps the findings that fire now.
func firing(fs []*v1.Finding) []*v1.Finding {
	var out []*v1.Finding
	for _, f := range fs {
		if f.GetState() != v1.FindingState_FINDING_STATE_RESOLVED {
			out = append(out, f)
		}
	}
	return out
}

func keepFinding(f *v1.Finding, req *v1.ListFindingsRequest) bool {
	if len(req.GetStates()) == 0 {
		if f.GetState() == v1.FindingState_FINDING_STATE_RESOLVED {
			return false
		}
	} else if !slices.Contains(req.GetStates(), f.GetState()) {
		return false
	}
	if len(req.GetSeverities()) > 0 && !slices.Contains(req.GetSeverities(), f.GetSeverity()) {
		return false
	}
	if f.GetMute() != nil && !req.GetIncludeMuted() {
		return false
	}
	if req.GetCheckCode() != "" && f.GetCheckCode() != req.GetCheckCode() {
		return false
	}
	return matchesEntity(f.GetEntity(), req.GetEntity())
}

// ListFindings filters the findings of the range and pages through them.
// A finding's state is its state at the range's end.
func (p *Provider) ListFindings(ctx context.Context, req *v1.ListFindingsRequest) (*v1.ListFindingsResponse, error) {
	all, err := p.findings(ctx, req.GetRange())
	if err != nil {
		return nil, err
	}
	var out []*v1.Finding
	for _, f := range all {
		if keepFinding(f, req) {
			out = append(out, f)
		}
	}
	page, next, err := api.Page(out, req.GetPageToken(), req.GetPageSize())
	if err != nil {
		return nil, err
	}
	return &v1.ListFindingsResponse{Findings: page, NextPageToken: next, TotalCount: int32(len(out))}, nil
}

// GetFinding reads one finding with its newest 100 transitions.
func (p *Provider) GetFinding(ctx context.Context, req *v1.GetFindingRequest) (*v1.GetFindingResponse, error) {
	f, err := p.St.Finding(ctx, req.GetId(), p.Now())
	if errors.Is(err, store.ErrFindingNotFound) {
		return nil, fmt.Errorf("finding %q: %w", req.GetId(), api.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	fs, err := p.findingProtos(ctx, []store.Finding{f})
	if err != nil {
		return nil, err
	}
	trs, err := p.St.FindingTransitions(ctx, store.TransitionQuery{FindingID: f.ID, Limit: 100})
	if err != nil {
		return nil, err
	}
	res := &v1.GetFindingResponse{Finding: fs[0]}
	for _, t := range trs {
		res.Transitions = append(res.Transitions, &v1.FindingTransition{FindingId: t.FindingID, Time: ts(t.Time),
			State: transitionState[t.Kind], Severity: severityOfText[t.Severity], Detail: t.Detail})
	}
	return res, nil
}

// statusOf is the status the firing findings give an entity, with their
// codes worst first; a muted finding's code ends in " muted". With no
// finding the entity is ok.
func statusOf(fs []*v1.Finding, e *v1.EntityRef) (v1.Status, []string) {
	st := v1.Status_STATUS_OK
	var codes []string
	for _, f := range fs {
		if !matchesEntity(f.GetEntity(), e) {
			continue
		}
		fst := statusOfSeverity(f.GetSeverity())
		code := f.GetCheckCode()
		if f.GetMute() != nil {
			fst, code = v1.Status_STATUS_MUTED, code+" muted"
		}
		if statusRank(fst) < statusRank(st) {
			st = fst
		}
		// A stream lists a code once, however many of its consumers have it.
		if !slices.Contains(codes, code) {
			codes = append(codes, code)
		}
	}
	return st, codes
}

func statusOfSeverity(s v1.Severity) v1.Status {
	switch s {
	case v1.Severity_SEVERITY_CRITICAL:
		return v1.Status_STATUS_CRITICAL
	case v1.Severity_SEVERITY_WARNING:
		return v1.Status_STATUS_WARNING
	case v1.Severity_SEVERITY_INFO:
		return v1.Status_STATUS_INFO
	}
	return v1.Status_STATUS_OK
}

// statusRank orders statuses worst first: muted after info, no data last.
func statusRank(s v1.Status) int {
	switch s {
	case v1.Status_STATUS_CRITICAL:
		return 0
	case v1.Status_STATUS_WARNING:
		return 1
	case v1.Status_STATUS_INFO:
		return 2
	case v1.Status_STATUS_MUTED:
		return 3
	case v1.Status_STATUS_OK:
		return 4
	}
	return 5
}

// worse returns the worse of two statuses.
func worse(a, b v1.Status) v1.Status {
	if statusRank(b) < statusRank(a) {
		return b
	}
	return a
}

// tsp is ts for an optional time.
func tsp(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return ts(*t)
}
