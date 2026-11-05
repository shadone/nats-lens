package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"
)

// notifyTables hold the notifier's state and outbox. They are not
// archived, and SweepNotifications, not Sweep, removes their rows.
var notifyTables = []string{"notify_channels", "notify_state", "notify_outbox"}

// States of a notify_state row.
const (
	NotifyFiring   = "firing"
	NotifyFlapping = "flapping"
)

// States of an outbox row.
const (
	OutboxPending   = "pending"
	OutboxDelivered = "delivered"
	OutboxRefused   = "refused"
	OutboxFailed    = "failed"
)

// OutboxKeep is the longest a delivered, refused or failed outbox row is
// kept; a shorter retention keeps it for retention only.
const OutboxKeep = 7 * 24 * time.Hour

// NotifyChannel is one row of notify_channels.
type NotifyChannel struct {
	Name            string
	BaselinedAt     time.Time
	BaselinedEpoch  int64
	ReconciledAt    time.Time
	ReconciledEpoch int64
}

// NotifyState is one row of notify_state: what a channel was last told
// about a finding.
type NotifyState struct {
	Channel    string
	FindingID  string
	SinceEpoch int64
	Severity   string
	// State is NotifyFiring or NotifyFlapping.
	State  string
	SentAt time.Time
	Epoch  int64
}

// NotifyKey names a notify_state row.
type NotifyKey struct {
	Channel, FindingID string
}

// OutboxRow is one row of notify_outbox.
type OutboxRow struct {
	ID        int64
	Channel   string
	CreatedAt time.Time
	Epoch     int64
	Kind      string
	// FindingID is empty for a baseline message.
	FindingID string
	DedupKey  string
	// Message is the message as JSON.
	Message       json.RawMessage
	State         string
	Attempts      int
	NextAttemptAt time.Time
	DeliveredAt   time.Time
	// LastStatus is 0 when no answer came.
	LastStatus int
	LastError  string
}

// NotifyInput is what a reconcile reads: the epoch, the findings that
// fire, flap or are known to a channel, with the mutes in force at the
// epoch's start applied, every mute, and the notifier's state.
type NotifyInput struct {
	Epoch int64
	At    time.Time
	// Findings by ID.
	Findings map[string]Finding
	// Mutes are every mute, newest first, ended ones included.
	Mutes []Mute
	// Channels by name.
	Channels map[string]NotifyChannel
	// States by channel, then finding ID.
	States map[string]map[string]NotifyState
	// AccountNames maps account IDs to readable names other than the ID.
	AccountNames map[string]string
}

// NotifyInput reads the reconcile's input for the epoch in a handful of
// queries, without the write lock.
func (s *Store) NotifyInput(ctx context.Context, epoch int64) (NotifyInput, error) {
	in := NotifyInput{Epoch: epoch, Findings: map[string]Finding{}, Channels: map[string]NotifyChannel{},
		States: map[string]map[string]NotifyState{}, AccountNames: map[string]string{}}
	at, err := s.EpochStartedAt(ctx, epoch)
	if err != nil {
		return in, err
	}
	in.At = at
	rows, err := s.db.QueryContext(ctx, `SELECT `+findingCols+` FROM findings
WHERE state = 'firing' OR flapping OR id IN (SELECT finding_id FROM notify_state)`)
	if err != nil {
		return in, fmt.Errorf("notify findings: %w", err)
	}
	var fs []Finding
	for rows.Next() {
		f, err := scanFinding(rows)
		if err != nil {
			rows.Close()
			return in, err
		}
		fs = append(fs, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return in, err
	}
	all, _, err := s.ListMutes(ctx, at, true, 1<<30, 0)
	if err != nil {
		return in, fmt.Errorf("notify mutes: %w", err)
	}
	in.Mutes = all
	var active []Mute
	for _, m := range all {
		if m.ActiveAt(at) {
			active = append(active, m)
		}
	}
	for _, f := range fs {
		f.Mute = muteFor(active, f)
		in.Findings[f.ID] = f
	}
	if in.Channels, err = s.NotifyChannels(ctx); err != nil {
		return in, err
	}
	srows, err := s.db.QueryContext(ctx, `SELECT channel, finding_id, since_epoch, severity, state, sent_at, epoch FROM notify_state`)
	if err != nil {
		return in, fmt.Errorf("notify state: %w", err)
	}
	for srows.Next() {
		var st NotifyState
		if err := srows.Scan(&st.Channel, &st.FindingID, &st.SinceEpoch, &st.Severity, &st.State, &st.SentAt, &st.Epoch); err != nil {
			srows.Close()
			return in, err
		}
		st.SentAt = st.SentAt.UTC()
		if in.States[st.Channel] == nil {
			in.States[st.Channel] = map[string]NotifyState{}
		}
		in.States[st.Channel][st.FindingID] = st
	}
	srows.Close()
	if err := srows.Err(); err != nil {
		return in, err
	}
	arows, err := s.db.QueryContext(ctx, `SELECT account, any_value(account_name) FROM accounts
WHERE account_name IS NOT NULL AND account_name <> account GROUP BY account`)
	if err != nil {
		return in, fmt.Errorf("account names: %w", err)
	}
	defer arows.Close()
	for arows.Next() {
		var acc, name string
		if err := arows.Scan(&acc, &name); err != nil {
			return in, err
		}
		in.AccountNames[acc] = name
	}
	return in, arows.Err()
}

// NotifyChannels returns the rows of notify_channels by name.
func (s *Store) NotifyChannels(ctx context.Context) (map[string]NotifyChannel, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT channel, baselined_at, baselined_epoch, reconciled_at, reconciled_epoch FROM notify_channels`)
	if err != nil {
		return nil, fmt.Errorf("notify channels: %w", err)
	}
	defer rows.Close()
	out := map[string]NotifyChannel{}
	for rows.Next() {
		var c NotifyChannel
		if err := rows.Scan(&c.Name, &c.BaselinedAt, &c.BaselinedEpoch, &c.ReconciledAt, &c.ReconciledEpoch); err != nil {
			return nil, err
		}
		c.BaselinedAt, c.ReconciledAt = c.BaselinedAt.UTC(), c.ReconciledAt.UTC()
		out[c.Name] = c
	}
	return out, rows.Err()
}

// NotifyChange is what one reconcile writes.
type NotifyChange struct {
	// Channels are upserted: each reconciled channel with its new
	// reconciled epoch, and a new one with its baseline.
	Channels []NotifyChannel
	// Upserts are written over the rows with their keys.
	Upserts []NotifyState
	// Deletes are removed; a key is never in both lists.
	Deletes []NotifyKey
	// Outbox rows are queued in this order; ID and State are ignored.
	Outbox []OutboxRow
}

// ErrNotifyReconciled is returned when a channel of the change was
// reconciled for the epoch, or a later one, already.
var ErrNotifyReconciled = errors.New("channel already reconciled for the epoch")

type jsonNotifyState struct {
	Channel    string `json:"channel"`
	FindingID  string `json:"finding_id"`
	SinceEpoch int64  `json:"since_epoch"`
	Severity   string `json:"severity"`
	State      string `json:"state"`
	SentAt     string `json:"sent_at"`
	Epoch      int64  `json:"epoch"`
}

const notifyStateShape = `[{"channel":"VARCHAR","finding_id":"VARCHAR","since_epoch":"BIGINT","severity":"VARCHAR",` +
	`"state":"VARCHAR","sent_at":"TIMESTAMP","epoch":"BIGINT"}]`

type jsonOutbox struct {
	ID        int64   `json:"id"`
	Channel   string  `json:"channel"`
	CreatedAt string  `json:"created_at"`
	Epoch     int64   `json:"epoch"`
	Kind      string  `json:"kind"`
	FindingID *string `json:"finding_id"`
	DedupKey  string  `json:"dedup_key"`
	Message   string  `json:"message"`
}

const outboxShape = `[{"id":"BIGINT","channel":"VARCHAR","created_at":"TIMESTAMP","epoch":"BIGINT","kind":"VARCHAR",` +
	`"finding_id":"VARCHAR","dedup_key":"VARCHAR","message":"VARCHAR"}]`

// microText formats t for a from_json TIMESTAMP field: the text is typed
// by the shape, so the session time zone does not apply.
func microText(t time.Time) string { return t.UTC().Format("2006-01-02 15:04:05.999999") }

// ApplyNotify writes one reconcile in one transaction: the channels, the
// state rows and the outbox rows, in the order given. It refuses the whole
// change, writing nothing, when a channel in it was already reconciled for
// an epoch at or after the one it records.
func (s *Store) ApplyNotify(ctx context.Context, ch NotifyChange) error {
	if err := s.lock(ctx); err != nil {
		return err
	}
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, c := range ch.Channels {
		var done sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT max(reconciled_epoch) FROM notify_channels WHERE channel = ?`, c.Name).Scan(&done); err != nil {
			return fmt.Errorf("notify channel %s: %w", c.Name, err)
		}
		if done.Valid && done.Int64 >= c.ReconciledEpoch {
			return fmt.Errorf("channel %s, epoch %d: %w", c.Name, c.ReconciledEpoch, ErrNotifyReconciled)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO notify_channels (channel, baselined_at, baselined_epoch, reconciled_at, reconciled_epoch)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (channel) DO UPDATE SET reconciled_at = excluded.reconciled_at, reconciled_epoch = excluded.reconciled_epoch`,
			c.Name, c.BaselinedAt.UTC(), c.BaselinedEpoch, c.ReconciledAt.UTC(), c.ReconciledEpoch); err != nil {
			return fmt.Errorf("notify channel %s: %w", c.Name, err)
		}
	}
	if len(ch.Deletes) > 0 {
		keys := make([]string, len(ch.Deletes))
		for i, k := range ch.Deletes {
			keys[i] = k.Channel + keySep + k.FindingID
		}
		for b := range slices.Chunk(keys, batchRows) {
			if _, err := tx.ExecContext(ctx, `DELETE FROM notify_state WHERE channel || chr(31) || finding_id IN (
    SELECT unnest(from_json(?::VARCHAR, '["VARCHAR"]')))`, jsonList(b)); err != nil {
				return fmt.Errorf("delete notify state: %w", err)
			}
		}
	}
	if len(ch.Upserts) > 0 {
		rows := make([]jsonNotifyState, len(ch.Upserts))
		for i, u := range ch.Upserts {
			rows[i] = jsonNotifyState{Channel: u.Channel, FindingID: u.FindingID, SinceEpoch: u.SinceEpoch, Severity: u.Severity,
				State: u.State, SentAt: microText(u.SentAt), Epoch: u.Epoch}
		}
		for b := range slices.Chunk(rows, batchRows) {
			if _, err := tx.ExecContext(ctx, `INSERT INTO notify_state (channel, finding_id, since_epoch, severity, state, sent_at, epoch)
SELECT channel, finding_id, since_epoch, severity, state, sent_at, epoch `+fromJSON(notifyStateShape)+`
ON CONFLICT (channel, finding_id) DO UPDATE SET since_epoch = excluded.since_epoch, severity = excluded.severity,
    state = excluded.state, sent_at = excluded.sent_at, epoch = excluded.epoch`, jsonList(b)); err != nil {
				return fmt.Errorf("write notify state: %w", err)
			}
		}
	}
	if len(ch.Outbox) > 0 {
		rows := make([]jsonOutbox, len(ch.Outbox))
		for i, o := range ch.Outbox {
			rows[i] = jsonOutbox{Channel: o.Channel, CreatedAt: microText(o.CreatedAt), Epoch: o.Epoch, Kind: o.Kind,
				DedupKey: o.DedupKey, Message: string(o.Message)}
			if o.FindingID != "" {
				id := o.FindingID
				rows[i].FindingID = &id
			}
		}
		// The IDs come from the sequence, in the order of the rows, so that
		// a channel's messages are sent as they were queued.
		ids, err := nextIDs(ctx, tx, "notify_outbox_seq", len(rows))
		if err != nil {
			return fmt.Errorf("queue notifications: %w", err)
		}
		for i := range rows {
			rows[i].ID = ids[i]
		}
		for b := range slices.Chunk(rows, batchRows) {
			if _, err := tx.ExecContext(ctx, `INSERT INTO notify_outbox (id, channel, created_at, epoch, kind, finding_id, dedup_key, message)
SELECT id, channel, created_at, epoch, kind, finding_id, dedup_key, message::JSON `+fromJSON(outboxShape), jsonList(b)); err != nil {
				return fmt.Errorf("queue notifications: %w", err)
			}
		}
	}
	return tx.Commit()
}

const outboxCols = `id, channel, created_at, epoch, kind, coalesce(finding_id, ''), dedup_key, CAST(message AS VARCHAR), state, attempts,
next_attempt_at, delivered_at, coalesce(last_status, 0), coalesce(last_error, '')`

func scanOutbox(row interface{ Scan(...any) error }) (OutboxRow, error) {
	var o OutboxRow
	var msg string
	var next, delivered sql.NullTime
	if err := row.Scan(&o.ID, &o.Channel, &o.CreatedAt, &o.Epoch, &o.Kind, &o.FindingID, &o.DedupKey, &msg, &o.State, &o.Attempts,
		&next, &delivered, &o.LastStatus, &o.LastError); err != nil {
		return OutboxRow{}, err
	}
	o.Message = json.RawMessage(msg)
	o.CreatedAt = o.CreatedAt.UTC()
	if next.Valid {
		o.NextAttemptAt = next.Time.UTC()
	}
	if delivered.Valid {
		o.DeliveredAt = delivered.Time.UTC()
	}
	return o, nil
}

// PendingOutbox returns up to limit pending rows of the channel, oldest
// first.
func (s *Store) PendingOutbox(ctx context.Context, channel string, limit int) ([]OutboxRow, error) {
	return s.outbox(ctx, `WHERE channel = ? AND state = 'pending' ORDER BY id LIMIT ?`, channel, limit)
}

// OutboxRows returns every outbox row of the channel, oldest first; every
// channel's when channel is empty.
func (s *Store) OutboxRows(ctx context.Context, channel string) ([]OutboxRow, error) {
	if channel == "" {
		return s.outbox(ctx, `ORDER BY id`)
	}
	return s.outbox(ctx, `WHERE channel = ? ORDER BY id`, channel)
}

func (s *Store) outbox(ctx context.Context, where string, args ...any) ([]OutboxRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+outboxCols+` FROM notify_outbox `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("read outbox: %w", err)
	}
	defer rows.Close()
	var out []OutboxRow
	for rows.Next() {
		o, err := scanOutbox(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// NotifySweep reports what SweepNotifications removed.
type NotifySweep struct {
	// Outbox counts delivered, refused and failed rows removed for age.
	Outbox int64
	// States counts notify_state rows of findings that no longer exist.
	States int64
	// Channels are the channels no longer configured whose rows were
	// removed, sorted; Dropped counts their pending messages removed with
	// them.
	Channels []string
	Dropped  int64
}

// SweepNotifications removes, in one transaction, delivered, refused and
// failed outbox rows created before now minus the shorter of OutboxKeep
// and retention, notify_state rows of findings that no longer exist, and
// every row of a channel not in keep (its notify_channels and
// notify_state rows and its outbox rows, pending ones included, as no
// channel is left to send them to).
func (s *Store) SweepNotifications(ctx context.Context, now time.Time, retention time.Duration, keep []string) (NotifySweep, error) {
	var res NotifySweep
	if err := s.lock(ctx); err != nil {
		return res, err
	}
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return res, err
	}
	defer func() { _ = tx.Rollback() }()
	age := OutboxKeep
	if retention > 0 && retention < age {
		age = retention
	}
	r, err := tx.ExecContext(ctx, `DELETE FROM notify_outbox WHERE state <> 'pending' AND created_at < ?`, now.Add(-age).UTC())
	if err != nil {
		return res, fmt.Errorf("sweep outbox: %w", err)
	}
	res.Outbox, _ = r.RowsAffected()
	if r, err = tx.ExecContext(ctx, `DELETE FROM notify_state WHERE finding_id NOT IN (SELECT id FROM findings)`); err != nil {
		return res, fmt.Errorf("sweep notify state: %w", err)
	}
	res.States, _ = r.RowsAffected()
	if keep == nil {
		keep = []string{}
	}
	kept := jsonList(keep)
	const gone = `channel NOT IN (SELECT unnest(from_json(?::VARCHAR, '["VARCHAR"]')))`
	rows, err := tx.QueryContext(ctx, `SELECT channel FROM notify_channels WHERE `+gone+`
UNION SELECT channel FROM notify_state WHERE `+gone+`
UNION SELECT channel FROM notify_outbox WHERE `+gone+` ORDER BY 1`, kept, kept, kept)
	if err != nil {
		return res, fmt.Errorf("sweep notify channels: %w", err)
	}
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			rows.Close()
			return res, err
		}
		res.Channels = append(res.Channels, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return res, err
	}
	if len(res.Channels) > 0 {
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM notify_outbox WHERE state = 'pending' AND `+gone, kept).Scan(&res.Dropped); err != nil {
			return res, err
		}
		for _, t := range notifyTables {
			if _, err := tx.ExecContext(ctx, `DELETE FROM `+t+` WHERE `+gone, kept); err != nil {
				return res, fmt.Errorf("sweep %s: %w", t, err)
			}
		}
	}
	return res, tx.Commit()
}

// keySep joins a channel and a finding ID into one text; chr(31) in SQL.
const keySep = "\x1f"

// nextIDs takes n values of the sequence, in ascending order.
func nextIDs(ctx context.Context, tx *sql.Tx, seq string, n int) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, `SELECT nextval('`+seq+`') FROM range(?)`, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]int64, 0, n)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) != n {
		return nil, fmt.Errorf("sequence %s gave %d values, want %d", seq, len(out), n)
	}
	slices.Sort(out)
	return out, nil
}
