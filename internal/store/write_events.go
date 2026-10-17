package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/shadone/nats-lens/internal/events"
)

var eventCols = []string{
	"time", "received_at", "source", "type", "kind", "server_name", "account", "stream", "consumer",
	"client_name", "client_host", "client_user", "client_lang", "reason", "summary", "raw",
}

// WriteEvents stores a batch of events in one transaction.
func (s *Store) WriteEvents(ctx context.Context, evs []events.Event) error {
	if len(evs) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	q := fmt.Sprintf("INSERT INTO events (id, %s) VALUES (nextval('event_seq'), %s)",
		strings.Join(eventCols, ", "), strings.TrimSuffix(strings.Repeat("?, ", len(eventCols)), ", "))
	stmt, err := tx.PrepareContext(ctx, q)
	if err != nil {
		return fmt.Errorf("prepare events: %w", err)
	}
	defer stmt.Close()
	for _, e := range evs {
		var raw any
		if len(e.Raw) > 0 {
			raw = utf8(string(e.Raw))
		}
		if done, err := updateK8sEvent(ctx, tx, e, raw); err != nil {
			return err
		} else if done {
			continue
		}
		if _, err := stmt.ExecContext(ctx,
			e.Time.UTC(), e.ReceivedAt.UTC(), utf8(e.Source), nullIfEmpty(utf8(e.Type)), utf8(e.Kind),
			nullIfEmpty(utf8(e.ServerName)), nullIfEmpty(utf8(e.Account)), nullIfEmpty(utf8(e.Stream)),
			nullIfEmpty(utf8(e.Consumer)), nullIfEmpty(utf8(e.ClientName)), nullIfEmpty(utf8(e.ClientHost)),
			nullIfEmpty(utf8(e.ClientUser)), nullIfEmpty(utf8(e.ClientLang)), nullIfEmpty(utf8(e.Reason)),
			utf8(e.Summary), raw,
		); err != nil {
			return fmt.Errorf("insert event %s: %w", e.Kind, err)
		}
	}
	names := map[string]string{}
	for _, e := range evs {
		if e.Account != "" && e.AccountName != "" {
			names[e.Account] = utf8(e.AccountName)
		}
	}
	if err := learnAccountNames(ctx, tx, names); err != nil {
		return err
	}
	return tx.Commit()
}

// k8sEventKeptFor is how far back a Kubernetes Event's row is looked for
// to update; the API server keeps an Event for an hour by default, and the
// watcher remembers counts for three.
const k8sEventKeptFor = 3 * time.Hour

// updateK8sEvent updates the stored row of a Kubernetes Event (an event
// with source k8s whose raw has a uid and a count) instead of adding one
// per occurrence: the count, note, summary and receive time become the
// new ones, the time stays the first occurrence's and raw records it as
// first_seen. It reports whether a row was updated. The receive time
// moves the row to the hour of its latest occurrence, so both hours'
// event counts change and the archive exports both again.
func updateK8sEvent(ctx context.Context, tx *sql.Tx, e events.Event, raw any) (bool, error) {
	if e.Source != events.SourceK8s || raw == nil {
		return false, nil
	}
	var probe struct {
		UID   string `json:"uid"`
		Count *int64 `json:"count"`
	}
	if json.Unmarshal(e.Raw, &probe) != nil || probe.UID == "" || probe.Count == nil {
		return false, nil
	}
	res, err := tx.ExecContext(ctx, `
UPDATE events SET received_at = ?, summary = ?, reason = ?,
  raw = json_merge_patch(?::JSON, json_object('first_seen', strftime(events.time, '%Y-%m-%dT%H:%M:%S.%fZ'),
    'last_seen', ?::VARCHAR))
WHERE id = (SELECT max(id) FROM events WHERE source = 'k8s' AND received_at >= ?
  AND json_extract_string(raw, '$.uid') = ? AND json_extract(raw, '$.count') IS NOT NULL)`,
		e.ReceivedAt.UTC(), utf8(e.Summary), nullIfEmpty(utf8(e.Reason)), raw, e.Time.UTC().Format(time.RFC3339Nano),
		e.ReceivedAt.UTC().Add(-k8sEventKeptFor), probe.UID)
	if err != nil {
		return false, fmt.Errorf("update kubernetes event: %w", err)
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// LearnAccountNames names accounts that have no readable name yet; a name
// already known is never replaced.
func (s *Store) LearnAccountNames(ctx context.Context, names map[string]string) error {
	if len(names) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := learnAccountNames(ctx, tx, names); err != nil {
		return err
	}
	return tx.Commit()
}

func learnAccountNames(ctx context.Context, tx *sql.Tx, names map[string]string) error {
	for acc, name := range names {
		if name == "" || name == acc {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE accounts SET account_name = ?
WHERE account = ? AND (account_name IS NULL OR account_name = account)`, name, acc); err != nil {
			return fmt.Errorf("account name: %w", err)
		}
	}
	return nil
}

// utf8 replaces invalid UTF-8 with U+FFFD. The driver cannot bind invalid
// UTF-8, and a client name is whatever the client sent; one such event
// would otherwise fail its batch, and the retried batch every later one.
func utf8(s string) string {
	return strings.ToValidUTF8(s, "\uFFFD")
}

// LastActivity returns the latest time anything was stored: the end of the
// latest epoch or the receive time of the latest event. It is zero for an
// empty database.
func (s *Store) LastActivity(ctx context.Context) (time.Time, error) {
	var t *time.Time
	err := s.db.QueryRowContext(ctx, `SELECT greatest(
    (SELECT max(finished_at) FROM epochs),
    (SELECT max(received_at) FROM events))`).Scan(&t)
	if err != nil || t == nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}
