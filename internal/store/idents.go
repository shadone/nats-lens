package store

import (
	"database/sql"
	"errors"
	"sort"
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

func sortedOf[K comparable](m map[K]time.Time, less func(a, b K) bool) []K {
	out := make([]K, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return less(out[i], out[j]) })
	return out
}

// writeIdents upserts the identity tables for the epoch.
func (w *writer) writeIdents() {
	d := w.ids
	for _, acc := range sortedKeys(d.accounts) {
		a := d.accounts[acc]
		// Without a readable name in this epoch the identifier stands in
		// for a new row, and an existing row keeps the name it has.
		name := a.name
		if name == "" {
			name = acc
		}
		w.exec(`INSERT INTO accounts (account, account_name, is_system, first_epoch, last_epoch) VALUES (?, ?, ?, ?, ?)
ON CONFLICT (account) DO UPDATE SET
    account_name = CASE WHEN excluded.account_name = excluded.account THEN accounts.account_name
                        ELSE excluded.account_name END,
    is_system = excluded.is_system OR accounts.is_system, last_epoch = excluded.last_epoch`,
			acc, name, a.system, w.epoch, w.epoch)
	}
	streams := sortedOf(d.streams, func(a, b [2]string) bool { return a[0]+"\x00"+a[1] < b[0]+"\x00"+b[1] })
	for _, k := range streams {
		w.exec(`INSERT INTO streams (account, stream, created, first_epoch, last_epoch) VALUES (?, ?, ?, ?, ?)
ON CONFLICT (account, stream) DO UPDATE SET created = coalesce(excluded.created, streams.created),
    last_epoch = excluded.last_epoch`,
			k[0], k[1], ts(d.streams[k]), w.epoch, w.epoch)
	}
	consumers := sortedOf(d.consumers, func(a, b [3]string) bool {
		return strings.Join(a[:], "\x00") < strings.Join(b[:], "\x00")
	})
	for _, k := range consumers {
		w.exec(`INSERT INTO consumers (account, stream, consumer, created, first_epoch, last_epoch) VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (account, stream, consumer) DO UPDATE SET created = coalesce(excluded.created, consumers.created),
    last_epoch = excluded.last_epoch`,
			k[0], k[1], k[2], ts(d.consumers[k]), w.epoch, w.epoch)
	}
	for _, id := range sortedKeys(d.raft) {
		r := d.raft[id]
		w.exec(`INSERT INTO raft_groups (group_id, kind, account, stream, consumer, first_epoch, last_epoch) VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (group_id) DO UPDATE SET account = coalesce(excluded.account, raft_groups.account),
    stream = coalesce(excluded.stream, raft_groups.stream), consumer = coalesce(excluded.consumer, raft_groups.consumer),
    last_epoch = excluded.last_epoch`,
			id, raftKind(id), nullIfEmpty(r.account), nullIfEmpty(r.stream), nullIfEmpty(r.consumer), w.epoch, w.epoch)
	}
}
