package storeprov

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
	"google.golang.org/protobuf/proto"
)

// sourceOf maps events.source to the API's sources: system events and
// JetStream advisories are both NATS.
func sourceOf(s string) v1.EventSource {
	switch s {
	case "sys", "jetstream":
		return v1.EventSource_EVENT_SOURCE_NATS
	case "collector":
		return v1.EventSource_EVENT_SOURCE_COLLECTOR
	case "k8s":
		return v1.EventSource_EVENT_SOURCE_KUBERNETES
	case store.SourceFinding:
		return v1.EventSource_EVENT_SOURCE_FINDING
	}
	return v1.EventSource_EVENT_SOURCE_UNSPECIFIED
}

// storedSources lists the events.source values of an API source.
func storedSources(s v1.EventSource) []string {
	switch s {
	case v1.EventSource_EVENT_SOURCE_NATS:
		return []string{"sys", "jetstream"}
	case v1.EventSource_EVENT_SOURCE_COLLECTOR:
		return []string{"collector"}
	case v1.EventSource_EVENT_SOURCE_KUBERNETES:
		return []string{"k8s"}
	case v1.EventSource_EVENT_SOURCE_FINDING:
		return []string{store.SourceFinding}
	}
	return nil
}

// severityOf colours an event by its kind. The server deletes the
// internal consumers of mirrors and sources (JS_MIRROR_..., JS_SOURCE_...)
// on its own, so their deletes are routine.
func severityOf(kind, reason, consumer string) v1.Severity {
	switch {
	case kind == "consumer.deleted" && (strings.HasPrefix(consumer, "JS_MIRROR_") || strings.HasPrefix(consumer, "JS_SOURCE_")):
		return v1.Severity_SEVERITY_UNSPECIFIED
	case kind == "consumer.deleted", kind == "stream.deleted", strings.HasSuffix(kind, ".quorum_lost"),
		kind == "server.shutdown", kind == "server.out_of_storage", kind == "collector.gap":
		return v1.Severity_SEVERITY_CRITICAL
	case kind == "server.ocsp_peer_rejected", kind == "server.lameduck", kind == "consumer.max_deliveries", kind == "consumer.terminated",
		kind == "jetstream.api_limit_reached", kind == "collector.disconnected", kind == "collector.messages_dropped",
		kind == "client.disconnect" && strings.Contains(strings.ToLower(reason), "slow consumer"):
		return v1.Severity_SEVERITY_WARNING
	case kind == "k8s.pod.evicted", kind == "k8s.pod.not_ready", kind == "k8s.pod.restarted", kind == "k8s.pod.backoff",
		kind == "k8s.pod.failed_scheduling", kind == "k8s.pod.oom_killed", kind == "k8s.node.not_ready", kind == "k8s.node.drained",
		kind == "collector.k8s_gap", kind == "collector.k8s_not_watching":
		return v1.Severity_SEVERITY_WARNING
	}
	return v1.Severity_SEVERITY_UNSPECIFIED
}

// eventFilter builds the WHERE clause of a ListEvents request.
func eventFilter(req *v1.ListEventsRequest) (string, []any) {
	start, end := span(req.GetRange())
	where := []string{"e.time >= ?", "e.time < ?"}
	args := []any{start, end}
	if req.EventReadId != nil {
		where = append(where, "e.id<=?")
		args = append(args, *req.EventReadId)
	}
	if len(req.SelectedIds) > 0 {
		where = append(where, "e.id IN (SELECT unnest(?::BIGINT[]))")
		ids, _ := api.EventSelection(req.SelectedIds, req.EventReadId)
		args = append(args, ids)
	}
	if len(req.GetSources()) > 0 {
		var src []string
		for _, s := range req.GetSources() {
			src = append(src, storedSources(s)...)
		}
		if len(src) == 0 {
			where = append(where, "false")
		} else {
			where = append(where, "e.source IN (?"+strings.Repeat(", ?", len(src)-1)+")")
			for _, s := range src {
				args = append(args, s)
			}
		}
	}
	if len(req.GetKinds()) > 0 {
		where = append(where, "e.kind IN (?"+strings.Repeat(", ?", len(req.GetKinds())-1)+")")
		for _, k := range req.GetKinds() {
			args = append(args, k)
		}
	}
	if req.GetClientName() != "" {
		where, args = append(where, "e.client_name = ?"), append(args, req.GetClientName())
	}
	// Events name the account as configured, entities as the servers
	// report it; in operator mode these differ, so both are accepted.
	account := "(e.account = ? OR e.account = (SELECT any_value(account_name) FROM accounts WHERE account = ?))"
	ent := req.GetEntity()
	parts := strings.Split(ent.GetKey(), "/")
	switch ent.GetType() {
	case v1.EntityType_ENTITY_TYPE_SERVER:
		where, args = append(where, "e.server_name = ?"), append(args, ent.GetKey())
	case v1.EntityType_ENTITY_TYPE_ACCOUNT:
		where, args = append(where, account), append(args, ent.GetKey(), ent.GetKey())
	case v1.EntityType_ENTITY_TYPE_CLIENT:
		where, args = append(where, "e.client_name = ?"), append(args, ent.GetKey())
	case v1.EntityType_ENTITY_TYPE_STREAM:
		if len(parts) != 2 {
			return "false", nil
		}
		where, args = append(where, account, "e.stream = ?"), append(args, parts[0], parts[0], parts[1])
	case v1.EntityType_ENTITY_TYPE_CONSUMER:
		if len(parts) != 3 {
			return "false", nil
		}
		where = append(where, account, "e.stream = ?", "e.consumer = ?")
		args = append(args, parts[0], parts[0], parts[1], parts[2])
	case v1.EntityType_ENTITY_TYPE_CONNECTION:
		// Connect and disconnect payloads name the connection as client.id.
		if len(parts) != 2 {
			return "false", nil
		}
		where = append(where, "e.server_name = ?", "json_extract_string(e.raw, '$.client.id') = ?")
		args = append(args, parts[0], parts[1])
	}
	return strings.Join(where, " AND "), args
}

// eventCols are the columns scanEvent reads; for a finding event also the
// finding, severity and detail from raw.
const eventCols = `e.id, e.time, e.received_at, e.source, e.kind, e.server_name, e.account, e.stream, e.consumer,
  e.client_name, e.client_host, e.reason, e.summary,
  CASE WHEN e.source = 'finding' THEN json_extract_string(e.raw, '$.finding_id') END,
  CASE WHEN e.source = 'finding' THEN json_extract_string(e.raw, '$.severity') END,
  CASE WHEN e.source = 'finding' THEN json_extract_string(e.raw, '$.detail') END,
 CASE WHEN e.source = 'finding' THEN TRY_CAST(json_extract_string(e.raw, '$.epoch') AS BIGINT) END`

func scanEvent(rows interface{ Scan(...any) error }, extra ...any) (*v1.Event, error) {
	var ev v1.Event
	var id int64
	var at, received time.Time
	var source string
	var server, account, stream, consumer, client, host, reason sql.NullString
	var findingID, findingSeverity, findingDetail sql.NullString
	var findingEpoch sql.NullInt64
	dest := append([]any{&id, &at, &received, &source, &ev.Kind, &server, &account, &stream, &consumer, &client, &host,
		&reason, &ev.Summary, &findingID, &findingSeverity, &findingDetail, &findingEpoch}, extra...)
	if err := rows.Scan(dest...); err != nil {
		return nil, err
	}
	ev.Id, ev.Time, ev.ReceivedAt, ev.Source = strconv.FormatInt(id, 10), ts(at.UTC()), ts(received.UTC()), sourceOf(source)
	ev.Type = strings.ToUpper(ev.GetKind())
	ev.Server, ev.Account, ev.Stream, ev.Consumer = server.String, account.String, stream.String, consumer.String
	ev.ClientName, ev.ClientHost, ev.Reason = client.String, host.String, reason.String
	ev.Severity = severityOf(ev.GetKind(), ev.GetReason(), ev.GetConsumer())
	if findingID.Valid {
		ev.Severity = severityOfText[findingSeverity.String]
		ev.Transition = &v1.FindingTransition{FindingId: findingID.String, Epoch: findingEpoch.Int64, Time: ev.GetTime(),
			State: transitionState[strings.TrimPrefix(ev.GetKind(), "finding.")], Severity: ev.GetSeverity(), Detail: findingDetail.String}
	}
	return &ev, nil
}

// decorateEvents fills what the findings add to events: a finding event
// gets its finding as it is now and the check's name in its type, and an
// event that caused findings gets them as raised.
func (p *Provider) decorateEvents(ctx context.Context, evs []*v1.Event) error {
	if len(evs) == 0 {
		return nil
	}
	// Only the findings of these events: their transitions' findings and
	// the findings they caused.
	q := store.FindingQuery{IDs: []string{}, CauseEventIDs: []int64{}}
	for _, ev := range evs {
		if t := ev.GetTransition(); t != nil {
			q.IDs = append(q.IDs, t.GetFindingId())
		}
		if id, err := strconv.ParseInt(ev.GetId(), 10, 64); err == nil {
			q.CauseEventIDs = append(q.CauseEventIDs, id)
		}
	}
	fs, err := p.St.Findings(ctx, p.Now(), q)
	if err != nil {
		return err
	}
	names, err := p.AccountNames(ctx)
	if err != nil {
		return err
	}
	byID := map[string]store.Finding{}
	byCause := map[string][]store.Finding{}
	for _, f := range fs {
		byID[f.ID] = f
		if f.CauseEventID > 0 {
			k := strconv.FormatInt(f.CauseEventID, 10)
			byCause[k] = append(byCause[k], f)
		}
	}
	for _, ev := range evs {
		if t := ev.GetTransition(); t != nil {
			if f, ok := byID[t.GetFindingId()]; ok {
				ev.Finding = p.findingProto(f, nil)
			}
			p.decorateEventLabel(ev, names)

		}
		for _, f := range byCause[ev.GetId()] {
			ev.Raised = append(ev.Raised, p.findingProto(f, nil))
		}
	}
	return nil
}

// Timeline's lane DTO does not contain current finding/raised-finding fields.
// Reuse pure labels without leaving the caller's transaction connection.
func (p *Provider) decorateTimelineEvents(ctx context.Context, evs []*v1.Event) error {
	if len(evs) == 0 {
		return nil
	}
	names, err := p.AccountNames(ctx)
	if err != nil {
		return err
	}
	for _, ev := range evs {
		p.decorateEventLabel(ev, names)
	}
	return nil
}

func (p *Provider) decorateEventLabel(ev *v1.Event, names map[string]string) {
	t := ev.GetTransition()
	if t == nil {
		return
	}
	code, _, _ := strings.Cut(t.GetFindingId(), ":")
	ev.Type = code
	if p.Checks != nil {
		if c, ok := p.Checks.Lookup(code); ok {
			ev.Type = code + " " + c.Name
		}
	}
	// The stored summary leads with the code for SQL readers; the
	// type carries that here, so the summary names the entity. A
	// resolve's detail is the last failing result, and says so.
	label := entityLabel(ev)
	if label == "" {
		// No column names the entity: fall back to the finding's
		// key, and for the system, whose key is empty, to its name.
		_, label, _ = strings.Cut(t.GetFindingId(), ":")
		if acc, rest, ok := strings.Cut(label, "/"); ok && names[acc] != "" {
			label = names[acc] + "/" + rest
		}
		if label == "" {
			label = "system"
		}
	}
	switch {
	case t.GetState() == v1.FindingState_FINDING_STATE_RESOLVED:
		ev.Summary = "resolved: " + label + ". Last failing result: " + t.GetDetail()
	case label != "":
		ev.Summary = label + ": " + t.GetDetail()
	default:
		ev.Summary = t.GetDetail()
	}
}

// ListEvents reads events newest first, paging by time and ID so that new
// events do not shift the pages.
func (p *Provider) ListEvents(ctx context.Context, req *v1.ListEventsRequest) (*v1.ListEventsResponse, error) {
	res, err := p.listEvents(ctx, req)
	if err == nil {
		res.SelectedIds = append([]string(nil), req.SelectedIds...)
		if req.EventReadId != nil {
			res.EventReadId = proto.Int64(*req.EventReadId)
		}
	}
	if err == nil && (len(req.SelectedIds) > 0 || req.EventReadId != nil) && !api.FitsEntityPage(res) {
		return nil, fmt.Errorf("%w: selected event response exceeds page budget", api.ErrResourceExhausted)
	}
	return res, err
}

func (p *Provider) listEvents(ctx context.Context, req *v1.ListEventsRequest) (*v1.ListEventsResponse, error) {
	if _, err := api.EventSelection(req.SelectedIds, req.EventReadId); err != nil {
		return nil, err
	}
	cur, err := api.DecodeCursor(req.GetPageToken())
	if err != nil {
		return nil, err
	}
	if richRequest(req) {
		return p.listEventsRich(ctx, req, cur)
	}
	where, args := eventFilter(req)
	res := &v1.ListEventsResponse{}
	if err := p.db().QueryRowContext(ctx, `SELECT count(*) FROM events e WHERE `+where, args...).Scan(&res.TotalCount); err != nil {
		return nil, fmt.Errorf("count events: %w", err)
	}
	page := where
	pargs := append([]any{}, args...)
	if cur.ID != 0 {
		page += " AND (e.time < ? OR (e.time = ? AND e.id < ?))"
		pargs = append(pargs, cur.Time.UTC(), cur.Time.UTC(), cur.ID)
	}
	rows, err := p.db().QueryContext(ctx, `SELECT `+eventCols+` FROM events e WHERE `+page+
		` ORDER BY e.time DESC, e.id DESC LIMIT ?`, append(pargs, req.GetPageSize()+1)...)
	if err != nil {
		return nil, fmt.Errorf("events: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		ev, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		res.Events = append(res.Events, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if n := int(req.GetPageSize()); len(res.Events) > n {
		res.Events = res.Events[:n]
		l := res.Events[n-1]
		id, _ := strconv.ParseInt(l.GetId(), 10, 64)
		res.NextPageToken = api.EncodeCursor(api.Cursor{Time: l.GetTime().AsTime(), ID: id})
	}
	return res, p.decorateEvents(ctx, res.Events)
}

// GetEvent reads one event with its payload.
func (p *Provider) GetEvent(ctx context.Context, req *v1.GetEventRequest) (*v1.GetEventResponse, error) {
	res, err := p.getEvent(ctx, req)
	if err == nil && req.EventReadId != nil {
		res.EventReadId = proto.Int64(*req.EventReadId)
	}
	if err == nil && req.EventReadId != nil && !api.FitsEntityPage(res) {
		return nil, fmt.Errorf("%w: selected event exceeds page budget", api.ErrResourceExhausted)
	}
	return res, err
}

func (p *Provider) getEvent(ctx context.Context, req *v1.GetEventRequest) (*v1.GetEventResponse, error) {
	if _, err := api.EventSelection(nil, req.EventReadId); err != nil {
		return nil, err
	}
	id, err := parseID(req.GetId())
	if err != nil {
		return nil, fmt.Errorf("%w: event id %q is not a positive number", api.ErrInvalid, req.GetId())
	}
	if req.EventReadId != nil && id > *req.EventReadId {
		return nil, fmt.Errorf("event %d outside selected membership: %w", id, api.ErrNotFound)
	}
	var raw sql.NullString
	ev, err := scanEvent(p.db().QueryRowContext(ctx, `SELECT `+eventCols+`, e.raw::VARCHAR FROM events e WHERE e.id = ?`, id), &raw)
	if isNoRows(err) {
		return nil, fmt.Errorf("event %d: %w", id, api.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	ev.Raw = raw.String
	return &v1.GetEventResponse{Event: ev}, p.decorateEvents(ctx, []*v1.Event{ev})
}

// laneKinds are the NATS events worth drawing on the timeline; findingKinds
// the finding transitions and acknowledgements, which have a limit of
// their own so that a burst of findings cannot push the NATS events out of
// the lane.
var (
	laneKinds    = api.NATSLaneKinds
	findingKinds = []string{"finding.firing", "finding.changed", "finding.resolved", store.AckEventKind}
)

// laneEvents reads the newest laneLimit events of the kinds in the range,
// and whether there were more.
func (p *Provider) laneEvents(ctx context.Context, r *v1.TimeRange, entity *v1.EntityRef, kinds []string, readIDs ...int64) ([]*v1.Event, bool, error) {
	request := &v1.ListEventsRequest{Range: r, Kinds: kinds, Entity: entity}
	if len(readIDs) > 0 {
		request.EventReadId = &readIDs[0]
	}
	where, args := eventFilter(request)
	// The newest laneLimit events, plus one to tell whether more exist.
	rows, err := p.db().QueryContext(ctx, `SELECT `+eventCols+` FROM events e WHERE `+where+
		` ORDER BY e.time DESC, e.id DESC LIMIT ?`, append(args, laneLimit+1)...)
	if err != nil {
		return nil, false, fmt.Errorf("lane events: %w", err)
	}
	defer rows.Close()
	var evs []*v1.Event
	for rows.Next() {
		ev, err := scanEvent(rows)
		if err != nil {
			return nil, false, err
		}
		if len(evs) == laneLimit {
			return evs, true, nil
		}
		evs = append(evs, ev)
	}
	return evs, false, rows.Err()
}

// lanes reads the NATS events and the finding transitions of the range,
// each the newest laneLimit, merged oldest first, with a truncation flag
// for each.
func (p *Provider) lanes(ctx context.Context, r *v1.TimeRange, entity *v1.EntityRef, readIDs ...int64) ([]*v1.LaneEvent, bool, bool, error) {
	nats, natsCut, err := p.laneEvents(ctx, r, entity, laneKinds, readIDs...)
	if err != nil {
		return nil, false, false, err
	}
	found, foundCut, err := p.laneEvents(ctx, r, entity, findingKinds, readIDs...)
	if err != nil {
		return nil, false, false, err
	}
	evs := append(nats, found...)
	if err := p.decorateTimelineEvents(ctx, evs); err != nil {
		return nil, false, false, err
	}
	slices.SortStableFunc(evs, func(a, b *v1.Event) int {
		if c := a.GetTime().AsTime().Compare(b.GetTime().AsTime()); c != 0 {
			return c
		}
		ai, _ := strconv.ParseInt(a.GetId(), 10, 64)
		bi, _ := strconv.ParseInt(b.GetId(), 10, 64)
		return cmp.Compare(ai, bi)
	})
	out := make([]*v1.LaneEvent, 0, len(evs))
	for _, ev := range evs {
		out = append(out, &v1.LaneEvent{Id: ev.GetId(), Time: ev.GetTime(), Source: ev.GetSource(), Kind: ev.GetKind(),
			ShortLabel: ev.GetType(), Label: ev.GetType() + " " + ev.GetSummary(), Severity: ev.GetSeverity(), EventId: ev.GetId(), Entity: api.EventEntity(ev), FindingId: ev.GetTransition().GetFindingId(), TransitionEpoch: ev.GetTransition().GetEpoch()})
	}
	return out, natsCut, foundCut, nil
}

// laneLimit is the most events the NATS lane of the timeline holds.
const laneLimit = 1000

// entityLabel names a finding event's entity the way a reader says it:
// "consumer on stream", the stream, the server or the client.
func entityLabel(ev *v1.Event) string {
	switch {
	case ev.GetConsumer() != "":
		return ev.GetConsumer() + " on " + ev.GetStream()
	case ev.GetStream() != "":
		return ev.GetStream()
	case ev.GetServer() != "":
		return ev.GetServer()
	case ev.GetClientName() != "":
		return ev.GetClientName()
	}
	return ev.GetAccount()
}
