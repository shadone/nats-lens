package storeprov

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
	"google.golang.org/protobuf/proto"
	"strings"
)

func (p *Provider) ListMetricContributors(ctx context.Context, in *v1.ListMetricContributorsRequest) (*v1.ListMetricContributorsResponse, error) {
	r := proto.Clone(in).(*v1.ListMetricContributorsRequest)
	if err := api.NormalizeMetricContributors(r, p.RangeReference()); err != nil {
		return nil, err
	}
	return api.MetricContributorPage(ctx, &p.listPages, r, func() (*api.EntitySnapshot, error) {
		beginner, ok := p.db().(interface {
			BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
		})
		if !ok {
			return nil, fmt.Errorf("%w: metric capture transaction unavailable", api.ErrFailedPrecondition)
		}
		tx, err := beginner.BeginTx(store.ReadTx(ctx), nil)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback()
		view := &Provider{St: p.St, Interval: p.Interval, readDB: tx}
		rows, err := view.metricContributorCandidates(ctx, r)
		if err != nil {
			return nil, err
		}
		if len(rows) > 0 {
			if err = view.metricContributorObservations(ctx, r, rows); err != nil {
				return nil, err
			}
		}
		api.SortMetricContributors(rows, r)
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return &api.EntitySnapshot{MetricRows: rows, Range: r.Range, ReadAt: ts(p.Now())}, nil
	})
}

func (p *Provider) metricContributorCandidates(ctx context.Context, r *v1.ListMetricContributorsRequest) ([]*v1.MetricContributor, error) {
	kind := api.Metrics[r.Metric].Entity
	start, end := span(r.Range)
	var q string
	args := []any{start, end}
	switch kind {
	case v1.EntityType_ENTITY_TYPE_SERVER:
		q = `SELECT s.server_name,s.server_name,'','','',count(DISTINCT ss.server_id),count(DISTINCT ss.server_id) FILTER(WHERE s.start_time>=? AND s.start_time<?)
 FROM server_stats ss JOIN epochs e USING(epoch) JOIN servers s USING(server_id)
 WHERE e.started_at>=? AND e.started_at<? AND (?='' OR s.server_name=?) AND contains(lower(s.server_name),lower(?)) GROUP BY s.server_name ORDER BY s.server_name LIMIT ?`
		args = []any{start, end, start, end, r.Server, r.Server, r.Query, api.MaxListRankingRows + 1}
	case v1.EntityType_ENTITY_TYPE_STREAM, v1.EntityType_ENTITY_TYPE_CONSUMER:
		table, key, name, consumer := "stream_stats", "x.account||'/'||x.stream", "x.stream", "''"
		if kind == v1.EntityType_ENTITY_TYPE_CONSUMER {
			table = "consumer_stats"
			key += "||'/'||x.consumer"
			name = "x.consumer"
			consumer = "x.consumer"
		}
		q = `SELECT DISTINCT ` + key + `,` + name + `,x.account,x.stream,` + consumer + `,0,0 FROM ` + table + ` x JOIN epochs e USING(epoch)
 WHERE e.started_at>=? AND e.started_at<? AND (?='' OR x.account=?) AND (?='' OR x.stream=?) AND contains(lower(` + key + `),lower(?)) ORDER BY 1 LIMIT ?`
		args = append(args, r.Account, r.Account, r.Stream, r.Stream, r.Query, api.MaxListRankingRows+1)
	default:
		return nil, api.ErrInvalid
	}
	result, err := p.db().QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer result.Close()
	var rows []*v1.MetricContributor
	size := 0
	for result.Next() {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		if len(rows) >= api.MaxListRankingRows {
			return nil, fmt.Errorf("%w: metric ranking exceeds %d observed entities; narrow filters", api.ErrResourceExhausted, api.MaxListRankingRows)
		}
		c := &v1.MetricContributor{Entity: &v1.EntityRef{Type: kind}}
		e := c.Entity
		if err = result.Scan(&e.Key, &e.Name, &e.Account, &e.Stream, &e.Consumer, &c.ObservedServerIds, &c.ProcessStarts); err != nil {
			return nil, err
		}
		if kind == v1.EntityType_ENTITY_TYPE_SERVER {
			e.Server = e.Key
		}
		size += proto.Size(c)
		if size > api.MaxListContextBytes {
			return nil, fmt.Errorf("%w: metric identity capture exceeds byte budget", api.ErrResourceExhausted)
		}
		rows = append(rows, c)
	}
	return rows, result.Err()
}

// Reuse the exact observation SQL rather than defining a second ranking formula.
// The only substituted fragments are trusted entity-column identifiers. Time
// bounds and all caller filters remain bound parameters (ASVS 1.2.4).
func (p *Provider) metricContributorObservations(ctx context.Context, r *v1.ListMetricContributorsRequest, contributors []*v1.MetricContributor) error {
	kind := api.Metrics[r.Metric].Entity
	keys, names, accounts, streams, consumers := []string{}, []string{}, []string{}, []string{}, []string{}
	ids, starts := []int64{}, []int64{}
	for _, c := range contributors {
		e := c.Entity
		keys = append(keys, e.Key)
		names = append(names, e.Name)
		accounts = append(accounts, e.Account)
		streams = append(streams, e.Stream)
		consumers = append(consumers, e.Consumer)
		ids = append(ids, int64(c.ObservedServerIds))
		starts = append(starts, int64(c.ProcessStarts))
	}
	dummy := contributors[0].Entity
	q, args, counter, err := p.observationQuery(ctx, r.Metric, dummy, r.Range)
	if err != nil {
		return err
	}
	entityColumns := []string{"rank_entity.entity_key"}
	if kind != v1.EntityType_ENTITY_TYPE_SERVER {
		entityColumns = []string{"rank_entity.account", "rank_entity.stream"}
		if kind == v1.EntityType_ENTITY_TYPE_CONSUMER {
			entityColumns = append(entityColumns, "rank_entity.consumer")
		}
	}
	parts := strings.Split(q, "?")
	if len(parts) != len(args)+1 {
		return fmt.Errorf("metric query parameter contract changed")
	}
	var sqlText strings.Builder
	bound := []any{keys, names, accounts, streams, consumers, ids, starts}
	for i, arg := range args {
		sqlText.WriteString(parts[i])
		if i >= 2 && i < 2+len(entityColumns) {
			sqlText.WriteString(entityColumns[i-2])
		} else {
			sqlText.WriteByte('?')
			bound = append(bound, arg)
		}
	}
	sqlText.WriteString(parts[len(parts)-1])
	query := `WITH rank_entities AS (SELECT unnest(?::VARCHAR[]) AS entity_key,unnest(?::VARCHAR[]) AS name,unnest(?::VARCHAR[]) AS account,unnest(?::VARCHAR[]) AS stream,unnest(?::VARCHAR[]) AS consumer,unnest(?::BIGINT[]) AS ids,unnest(?::BIGINT[]) AS starts)
 SELECT rank_entity.entity_key,rank_entity.name,rank_entity.account,rank_entity.stream,rank_entity.consumer,rank_entity.ids,rank_entity.starts,observations.*
 FROM rank_entities rank_entity CROSS JOIN LATERAL (` + sqlText.String() + `) observations ORDER BY rank_entity.entity_key`
	result, err := p.db().QueryContext(ctx, query, bound...)
	if err != nil {
		return err
	}
	defer result.Close()
	loaded := make([]*v1.MetricContributor, 0, len(contributors))
	size := 0
	for result.Next() {
		if err = ctx.Err(); err != nil {
			return err
		}
		if len(loaded) >= len(contributors) {
			return fmt.Errorf("metric capture returned duplicate identities")
		}
		c := &v1.MetricContributor{Entity: &v1.EntityRef{Type: kind}}
		e := c.Entity
		c.Observations, err = scanObservations(result, counter, &e.Key, &e.Name, &e.Account, &e.Stream, &e.Consumer, &c.ObservedServerIds, &c.ProcessStarts)
		if err != nil {
			return err
		}
		if kind == v1.EntityType_ENTITY_TYPE_SERVER {
			e.Server = e.Key
		}
		if e.Key != contributors[len(loaded)].Entity.Key {
			return fmt.Errorf("metric capture identity changed")
		}
		size += proto.Size(c)
		if size > api.MaxListContextBytes {
			return fmt.Errorf("%w: metric summary capture exceeds byte budget; narrow filters", api.ErrResourceExhausted)
		}
		loaded = append(loaded, c)
	}
	if err = result.Err(); err != nil {
		return err
	}
	if len(loaded) != len(contributors) {
		return fmt.Errorf("metric capture omitted identities")
	}
	copy(contributors, loaded)
	return nil
}
