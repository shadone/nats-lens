package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// idents collects identity rows reported by any server in the epoch, so
// each is upserted once even when every server reports it.
type idents struct {
	accounts  map[string]accountIdent
	streams   map[[2]string]time.Time
	consumers map[[3]string]time.Time
	raft      map[string]raftIdent
}

type accountIdent struct {
	name   string
	system bool
}

type raftIdent struct {
	account, stream, consumer string
}

func newIdents() *idents {
	return &idents{
		accounts:  map[string]accountIdent{},
		streams:   map[[2]string]time.Time{},
		consumers: map[[3]string]time.Time{},
		raft:      map[string]raftIdent{},
	}
}

func (d *idents) account(acc, name string, system bool) {
	if acc == "" {
		return
	}
	cur := d.accounts[acc]
	if cur.name == "" || cur.name == acc {
		cur.name = name
	}
	cur.system = cur.system || system
	d.accounts[acc] = cur
}

func (d *idents) stream(account, stream string, created time.Time) {
	k := [2]string{account, stream}
	d.streams[k] = earlier(d.streams[k], created)
}

func (d *idents) consumer(account, stream, consumer string, created time.Time) {
	k := [3]string{account, stream, consumer}
	d.consumers[k] = earlier(d.consumers[k], created)
}

// raftGroup records a group seen in RAFTZ; it keeps an asset already known.
func (d *idents) raftGroup(id string) {
	if _, ok := d.raft[id]; !ok {
		d.raft[id] = raftIdent{}
	}
}

// raftAsset records which stream or consumer a group replicates, from JSZ.
func (d *idents) raftAsset(id, account, stream, consumer string) {
	d.raft[id] = raftIdent{account: account, stream: stream, consumer: consumer}
}

// earlier returns the earlier of two times, ignoring zero times.
func earlier(a, b time.Time) time.Time {
	if a.IsZero() || (!b.IsZero() && b.Before(a)) {
		return b
	}
	return a
}

func raftKind(id string) string {
	switch {
	case id == "_meta_":
		return "meta"
	case strings.HasPrefix(id, "S-"):
		return "stream"
	case strings.HasPrefix(id, "C-"):
		return "consumer"
	}
	return "other"
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func isNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }

type accountIdentRow struct {
	Account  string `json:"account"`
	Name     string `json:"account_name"`
	IsSystem bool   `json:"is_system"`
}

type streamIdentRow struct {
	Account string `json:"account"`
	Stream  string `json:"stream"`
	Created any    `json:"created"`
}

type consumerIdentRow struct {
	Account  string `json:"account"`
	Stream   string `json:"stream"`
	Consumer string `json:"consumer"`
	Created  any    `json:"created"`
}

type raftIdentRow struct {
	GroupID  string `json:"group_id"`
	Kind     string `json:"kind"`
	Account  any    `json:"account"`
	Stream   any    `json:"stream"`
	Consumer any    `json:"consumer"`
}

// writeIdents upserts the identity tables for the epoch, each in JSON
// batches. The idents maps hold one entry per key, so no batch holds a key
// twice, which ON CONFLICT would refuse. Creation times go as Unix
// microseconds made into TIMESTAMP in SQL.
func (w *writer) writeIdents() {
	d := w.ids
	accounts := make([]accountIdentRow, 0, len(d.accounts))
	for _, acc := range sortedKeys(d.accounts) {
		a := d.accounts[acc]
		// Without a readable name in this epoch the identifier stands in
		// for a new row, and an existing row keeps the name it has.
		name := a.name
		if name == "" {
			name = acc
		}
		accounts = append(accounts, accountIdentRow{Account: acc, Name: name, IsSystem: a.system})
	}
	streams := make([]streamIdentRow, 0, len(d.streams))
	for k, created := range d.streams {
		streams = append(streams, streamIdentRow{Account: k[0], Stream: k[1], Created: unixMicro(created)})
	}
	consumers := make([]consumerIdentRow, 0, len(d.consumers))
	for k, created := range d.consumers {
		consumers = append(consumers, consumerIdentRow{Account: k[0], Stream: k[1], Consumer: k[2], Created: unixMicro(created)})
	}
	groups := make([]raftIdentRow, 0, len(d.raft))
	for id, r := range d.raft {
		groups = append(groups, raftIdentRow{GroupID: id, Kind: raftKind(id), Account: nullIfEmpty(r.account),
			Stream: nullIfEmpty(r.stream), Consumer: nullIfEmpty(r.consumer)})
	}

	epochs := fmt.Sprintf("%d, %d", w.epoch, w.epoch)
	writeJSON(w, "accounts", `INSERT INTO accounts (account, account_name, is_system, first_epoch, last_epoch)
SELECT account, account_name, is_system, `+epochs+`
`+fromJSON(`[{"account":"VARCHAR","account_name":"VARCHAR","is_system":"BOOLEAN"}]`)+`
ON CONFLICT (account) DO UPDATE SET
    account_name = CASE WHEN excluded.account_name = excluded.account THEN accounts.account_name
                        ELSE excluded.account_name END,
    is_system = excluded.is_system OR accounts.is_system, last_epoch = excluded.last_epoch`, accounts, jsonBatch)
	writeJSON(w, "streams", `INSERT INTO streams (account, stream, created, first_epoch, last_epoch)
SELECT account, stream, make_timestamp(created), `+epochs+`
`+fromJSON(`[{"account":"VARCHAR","stream":"VARCHAR","created":"BIGINT"}]`)+`
ON CONFLICT (account, stream) DO UPDATE SET created = coalesce(excluded.created, streams.created),
    last_epoch = excluded.last_epoch`, streams, jsonBatch)
	writeJSON(w, "consumers", `INSERT INTO consumers (account, stream, consumer, created, first_epoch, last_epoch)
SELECT account, stream, consumer, make_timestamp(created), `+epochs+`
`+fromJSON(`[{"account":"VARCHAR","stream":"VARCHAR","consumer":"VARCHAR","created":"BIGINT"}]`)+`
ON CONFLICT (account, stream, consumer) DO UPDATE SET created = coalesce(excluded.created, consumers.created),
    last_epoch = excluded.last_epoch`, consumers, jsonBatch)
	writeJSON(w, "raft_groups", `INSERT INTO raft_groups (group_id, kind, account, stream, consumer, first_epoch, last_epoch)
SELECT group_id, kind, account, stream, consumer, `+epochs+`
`+fromJSON(`[{"group_id":"VARCHAR","kind":"VARCHAR","account":"VARCHAR","stream":"VARCHAR","consumer":"VARCHAR"}]`)+`
ON CONFLICT (group_id) DO UPDATE SET account = coalesce(excluded.account, raft_groups.account),
    stream = coalesce(excluded.stream, raft_groups.stream), consumer = coalesce(excluded.consumer, raft_groups.consumer),
    last_epoch = excluded.last_epoch`, groups, jsonBatch)
}
