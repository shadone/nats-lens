package storeprov

import (
	"context"
	"encoding/json"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/checks"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

func streamSummaryKeys(ss []*v1.StreamSummary) []string {
	keys := make([]string, 0, len(ss))
	for _, s := range ss {
		keys = append(keys, s.GetEntity().GetKey())
	}
	return keys
}
func consumerSummaryKeys(cs []*v1.ConsumerSummary) []string {
	keys := make([]string, 0, len(cs))
	for _, c := range cs {
		keys = append(keys, c.GetEntity().GetKey())
	}
	return keys
}

// listStatusFindings projects only ranking inputs: no finding details, configs,
// cause events, acknowledgements, or materialized mute lists. EXISTS preserves
// the muted status of either a check-wide or exact-entity active mute.
func (p *Provider) listStatusFindings(ctx context.Context, r *v1.TimeRange, keys []string, streams bool) ([]*v1.Finding, error) {
	scope := `((entity_type = 'consumer' AND entity_key IN (SELECT unnest(?::VARCHAR[])))
 OR (? AND entity_type IN ('stream','consumer') AND EXISTS
 (SELECT 1 FROM unnest(?::VARCHAR[]) k(key) WHERE entity_key = k.key OR
 (entity_type = 'consumer' AND starts_with(entity_key, k.key || '/')))))`
	start, end := span(r)
	at := p.Now()
	q := `SELECT id, check_code, entity_type, entity_key, severity, since FROM findings
 WHERE state = 'firing' AND since < ? AND (resolved_at IS NULL OR resolved_at >= ?) AND ` + scope
	args := []any{end, start, keys, streams, keys}
	if p.past(r) {
		at = end
		// The newest transition before end is firing iff FindingsAt would retain a
		// firing episode. Epoch, rather than time, resolves the same ties as the store.
		q = `WITH selected AS (SELECT * FROM finding_transitions WHERE time < ? AND ` + scope + `),
 last AS (SELECT finding_id, arg_max(struct_pack(check_code, entity_type, entity_key, severity, kind, time, epoch), epoch) AS t FROM selected GROUP BY finding_id)
 SELECT l.finding_id AS id, l.t.check_code AS check_code, l.t.entity_type AS entity_type,
 l.t.entity_key AS entity_key, l.t.severity AS severity,
 coalesce((SELECT max(s.time) FROM selected s WHERE s.finding_id=l.finding_id AND s.kind='firing' AND s.epoch <= l.t.epoch), l.t.time) AS since
 FROM last l WHERE l.t.kind <> 'resolved'`
		args = []any{end, keys, streams, keys}
	}
	// Unix microseconds avoid host-zone interpretation in the scalar CTE.
	q = `WITH firing AS (` + q + `), clock AS (SELECT make_timestamp(?::BIGINT) AS at)
 SELECT f.*, EXISTS (SELECT 1 FROM mutes m, clock c WHERE m.check_code=f.check_code
 AND (m.entity_type IS NULL OR (m.entity_type=f.entity_type AND m.entity_key=f.entity_key))
 AND m.created_at <= c.at AND m.expires_at > c.at AND (m.unmuted_at IS NULL OR m.unmuted_at > c.at)) AS muted
 FROM firing f ORDER BY f.id`
	args = append(args, at.UnixMicro())
	var out []*v1.Finding
	err := p.readMetadata(ctx, q, args, newMetadataBudget(), func(raw []byte) error {
		var v struct {
			ID         string `json:"id"`
			CheckCode  string `json:"check_code"`
			EntityType string `json:"entity_type"`
			EntityKey  string `json:"entity_key"`
			Severity   string `json:"severity"`
			Since      string `json:"since"`
			Muted      bool   `json:"muted"`
		}
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		since, err := time.Parse("2006-01-02 15:04:05.999999999", v.Since)
		if err != nil {
			return err
		}
		f := &v1.Finding{Id: v.ID, CheckCode: v.CheckCode, Entity: entityRef(v.EntityType, v.EntityKey), Severity: severityOfText[v.Severity], Since: ts(since), Expiry: checks.IsExpiry(v.CheckCode)}
		if v.Muted {
			f.Mute = &v1.Mute{}
		}
		out = append(out, f)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sortWorstFirst(out)
	return out, nil
}

func (p *Provider) listStreamInventory(ctx context.Context, keys []string) (map[string]int32, map[string]bool, error) {
	expected, named := map[string]int32{}, map[string]bool{}
	q := `WITH resolved AS (SELECT coalesce((SELECT a.account FROM accounts a WHERE i.account IN (a.account,a.account_name)
 ORDER BY (a.account=i.account) DESC LIMIT 1),i.account) AS account, i.stream, i.kind FROM inventory i)
 SELECT account || '/' || stream AS key, count(*) FILTER (WHERE kind='consumer') AS expected
 FROM resolved WHERE account || '/' || stream IN (SELECT unnest(?::VARCHAR[])) GROUP BY account,stream`
	err := p.readMetadata(ctx, q, []any{keys}, newMetadataBudget(), func(raw []byte) error {
		var v struct {
			Key      string
			Expected int32
		}
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		expected[v.Key], named[v.Key] = v.Expected, true
		return nil
	})
	return expected, named, err
}

func (p *Provider) listConsumerInventory(ctx context.Context, account, stream, consumer string) ([]expectedConsumer, error) {
	q := `WITH resolved AS (SELECT coalesce((SELECT a.account FROM accounts a WHERE i.account IN (a.account,a.account_name)
 ORDER BY (a.account=i.account) DESC LIMIT 1),i.account) AS account, i.stream, i.consumer, i.replicas FROM inventory i WHERE kind='consumer')
 SELECT * FROM resolved WHERE (?='' OR account=?) AND (?='' OR stream=?) AND (?='' OR consumer=?) ORDER BY account,stream,consumer`
	var out []expectedConsumer
	budget := newMetadataBudget()
	budget.rows = api.MaxListRankingRows
	err := p.readMetadata(ctx, q, []any{account, account, stream, stream, consumer, consumer}, budget, func(raw []byte) error {
		var v struct {
			Account, Stream, Consumer string
			Replicas                  *int32
		}
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		c := expectedConsumer{account: v.Account, stream: v.Stream, consumer: v.Consumer}
		if v.Replicas != nil {
			c.replicas.Int32, c.replicas.Valid = *v.Replicas, true
		}
		out = append(out, c)
		return nil
	})
	return out, err
}

func (p *Provider) listStreamStatus(ctx context.Context, r *v1.TimeRange, e epochInfo, ss []*v1.StreamSummary) error {
	keys := streamSummaryKeys(ss)
	cur, err := p.listStatusFindings(ctx, r, keys, true)
	if err != nil {
		return err
	}
	ran, err := p.checksRan(ctx, e.epoch)
	if err != nil {
		return err
	}
	expected, named, err := p.listStreamInventory(ctx, keys)
	if err != nil {
		return err
	}
	applyStreamStatus(ss, cur, ran, expected, named)
	return nil
}
