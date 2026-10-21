package store

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/nats-io/nats-server/v2/server"

	"github.com/shadone/nats-lens/internal/collect"
)

// subscriptionRow is one row of subscriptions: one connection's
// subscriptions with the same subject and queue, folded.
type subscriptionRow struct {
	Account string  `json:"account"`
	Subject string  `json:"subject"`
	Queue   *string `json:"queue"`
	Cid     uint64  `json:"cid"`
	Subs    int     `json:"subs"`
	Msgs    int64   `json:"msgs"`
}

const subscriptionShape = `[{"account":"VARCHAR","subject":"VARCHAR","queue":"VARCHAR","cid":"UBIGINT","subs":"INTEGER","msgs":"BIGINT"}]`

// subscriptionBatch is the most rows one INSERT carries.
const subscriptionBatch = 5000

// foldSubscriptions folds a SUBSZ list into one row per (account, subject,
// queue, cid), in that order.
func foldSubscriptions(subs []server.SubDetail) []subscriptionRow {
	type key struct {
		account, subject, queue string
		cid                     uint64
	}
	rows := map[key]*subscriptionRow{}
	for _, d := range subs {
		k := key{d.Account, d.Subject, d.Queue, d.Cid}
		r, ok := rows[k]
		if !ok {
			r = &subscriptionRow{Account: d.Account, Subject: d.Subject, Cid: d.Cid}
			if d.Queue != "" {
				q := d.Queue
				r.Queue = &q
			}
			rows[k] = r
		}
		r.Subs++
		r.Msgs += d.Msgs
	}
	out := make([]subscriptionRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Account != b.Account {
			return a.Account < b.Account
		}
		if a.Subject != b.Subject {
			return a.Subject < b.Subject
		}
		if qa, qb := deref(a.Queue), deref(b.Queue); qa != qb {
			return qa < qb
		}
		return a.Cid < b.Cid
	})
	return out
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// writeSubscriptions stores a server's SUBSZ reply: its list row and its
// subscriptions, folded, in batches handed to DuckDB as JSON, since a
// list holds up to subsz_limit rows.
func (w *writer) writeSubscriptions(v *collect.ServerView) {
	sz := v.Subsz
	if sz == nil {
		return
	}
	w.insert("subscription_lists", []string{"epoch", "server_id", "total", "listed"},
		w.epoch, v.Info.ID, int64(sz.Total), int64(len(sz.Subs)))
	rows := foldSubscriptions(sz.Subs)
	q := fmt.Sprintf(`INSERT INTO subscriptions (epoch, server_id, account, subject, queue, cid, subs, msgs)
SELECT %d, %s, account, subject, queue, cid, subs, msgs
FROM (SELECT unnest(from_json(?::VARCHAR, '%s'), recursive := true))`, w.epoch, sqlString(v.Info.ID), subscriptionShape)
	for len(rows) > 0 && w.err == nil {
		n := min(len(rows), subscriptionBatch)
		b, err := json.Marshal(rows[:n])
		if err != nil {
			w.fail(fmt.Errorf("subscriptions: %w", err))
			return
		}
		w.exec(q, string(b))
		rows = rows[n:]
	}
}
