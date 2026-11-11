package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Ack is one row of the acknowledgements table: a person works on one
// episode of a finding.
type Ack struct {
	ID         int64
	FindingID  string
	CheckCode  string
	EntityType string
	EntityKey  string
	// SinceEpoch is the episode: the finding's since_epoch when it was
	// acknowledged.
	SinceEpoch int64
	Note       string
	AckedBy    string
	CreatedAt  time.Time
	// QuietUntil is zero for a quiet until the finding resolves.
	QuietUntil time.Time
	// EndedAt is zero while nobody ended it.
	EndedAt   time.Time
	EndedBy   string
	EndReason string
	// ReadAt is the time a read chose it for its finding at: now, or a
	// past range's end. Zero on a row not read for a finding.
	ReadAt time.Time
}

// End reasons of an acknowledgement.
const (
	AckReleased = "released"
	AckReplaced = "replaced"
)

// Errors of the acknowledgement methods.
var (
	ErrAckNotFound = errors.New("acknowledgement not found")
	// ErrAckEnded is returned for an acknowledgement already ended or
	// whose episode is over.
	ErrAckEnded = errors.New("acknowledgement no longer in force")
	// ErrFindingNotFiring is returned for an acknowledgement of a
	// resolved finding.
	ErrFindingNotFiring = errors.New("finding is not firing")
)

// InForceAt reports whether the acknowledgement was made by t and not
// ended then. Whether its episode still runs is the finding's to say.
func (a Ack) InForceAt(t time.Time) bool {
	return !a.CreatedAt.After(t) && (a.EndedAt.IsZero() || a.EndedAt.After(t))
}

// QuietAt reports whether the acknowledgement's quiet holds at t: it is
// in force and its quiet has not run out.
func (a Ack) QuietAt(t time.Time) bool {
	return a.InForceAt(t) && (a.QuietUntil.IsZero() || a.QuietUntil.After(t))
}

// matches reports whether the acknowledgement is of f's current episode,
// which fires.
func (a Ack) matches(f Finding) bool {
	return f.Firing() && a.FindingID == f.ID && a.SinceEpoch == f.SinceEpoch
}

const ackCols = `id, finding_id, check_code, entity_type, entity_key, since_epoch, note, acked_by, created_at, quiet_until,
ended_at, ended_by, end_reason`

func scanAck(row interface{ Scan(...any) error }) (Ack, error) {
	var a Ack
	var quiet, ended sql.NullTime
	var by, reason sql.NullString
	if err := row.Scan(&a.ID, &a.FindingID, &a.CheckCode, &a.EntityType, &a.EntityKey, &a.SinceEpoch, &a.Note, &a.AckedBy,
		&a.CreatedAt, &quiet, &ended, &by, &reason); err != nil {
		return Ack{}, err
	}
	a.CreatedAt = a.CreatedAt.UTC()
	if quiet.Valid {
		a.QuietUntil = quiet.Time.UTC()
	}
	if ended.Valid {
		a.EndedAt = ended.Time.UTC()
	}
	a.EndedBy, a.EndReason = by.String, reason.String
	return a, nil
}

// nullTime is t for a TIMESTAMP column, or NULL when t is zero.
func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}

// Acknowledge stores an acknowledgement of the finding a.FindingID's
// current episode, made at a.CreatedAt, and returns it with its ID and
// the finding's identity. An earlier acknowledgement of the same episode
// still in force ends as replaced, by a.AckedBy. The acknowledgement is
// also written as a finding.acknowledged event, in the same transaction.
// It returns ErrFindingNotFound for an unknown finding and
// ErrFindingNotFiring for a resolved one.
func (s *Store) Acknowledge(ctx context.Context, a Ack) (Ack, error) {
	if err := s.lock(ctx, opAcks); err != nil {
		return Ack{}, err
	}
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Ack{}, err
	}
	defer func() { _ = tx.Rollback() }()
	f, err := scanFinding(tx.QueryRowContext(ctx, `SELECT `+findingCols+` FROM findings WHERE id = ?`, a.FindingID))
	if errors.Is(err, sql.ErrNoRows) {
		return Ack{}, ErrFindingNotFound
	}
	if err != nil {
		return Ack{}, err
	}
	if !f.Firing() {
		return Ack{}, ErrFindingNotFiring
	}
	a.CheckCode, a.EntityType, a.EntityKey, a.SinceEpoch = f.CheckCode, f.EntityType, f.EntityKey, f.SinceEpoch
	a.EndedAt, a.EndedBy, a.EndReason = time.Time{}, "", ""
	if _, err := tx.ExecContext(ctx, `UPDATE acknowledgements SET ended_at = ?, ended_by = ?, end_reason = ?
WHERE finding_id = ? AND since_epoch = ? AND ended_at IS NULL`,
		a.CreatedAt.UTC(), a.AckedBy, AckReplaced, a.FindingID, a.SinceEpoch); err != nil {
		return Ack{}, fmt.Errorf("replace acknowledgements of %s: %w", a.FindingID, err)
	}
	out, err := scanAck(tx.QueryRowContext(ctx, `INSERT INTO acknowledgements (id, finding_id, check_code, entity_type,
    entity_key, since_epoch, note, acked_by, created_at, quiet_until)
VALUES (nextval('ack_seq'), ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING `+ackCols,
		a.FindingID, a.CheckCode, a.EntityType, a.EntityKey, a.SinceEpoch, a.Note, a.AckedBy, a.CreatedAt.UTC(),
		nullTime(a.QuietUntil)))
	if err != nil {
		return Ack{}, fmt.Errorf("acknowledge %s: %w", a.FindingID, err)
	}
	if err := writeAckEvent(ctx, tx, out, f); err != nil {
		return Ack{}, err
	}
	if err := tx.Commit(); err != nil {
		return Ack{}, err
	}
	return out, nil
}

// AckEventKind is the events.kind of an acknowledgement.
const AckEventKind = "finding.acknowledged"

// AckDetail is what an acknowledgement says in its event: "acknowledged
// by <who>", then the note after a colon when there is one.
func AckDetail(by, note string) string {
	if note == "" {
		return "acknowledged by " + by
	}
	return "acknowledged by " + by + ": " + note
}

// writeAckEvent writes the acknowledgement as an event of source finding,
// so that the event list, the timeline and the finding's events show it.
// Its raw payload carries the finding and the severity as a transition's
// does, and its detail is AckDetail.
func writeAckEvent(ctx context.Context, tx *sql.Tx, a Ack, f Finding) error {
	var server, account, stream, consumer, client any
	parts := strings.Split(a.EntityKey, "/")
	switch {
	case a.EntityType == "server":
		server = a.EntityKey
	case a.EntityType == "client":
		client = a.EntityKey
	case a.EntityType == "account":
		account = a.EntityKey
	case a.EntityType == "stream" && len(parts) == 2:
		account, stream = parts[0], parts[1]
	case a.EntityType == "consumer" && len(parts) == 3:
		account, stream, consumer = parts[0], parts[1], parts[2]
	}
	detail := AckDetail(a.AckedBy, a.Note)
	var quiet any
	if !a.QuietUntil.IsZero() {
		quiet = a.QuietUntil.Format(time.RFC3339)
	}
	raw, err := json.Marshal(map[string]any{
		"finding_id": a.FindingID, "check_code": a.CheckCode, "entity_type": a.EntityType, "entity_key": a.EntityKey,
		"severity": f.Severity, "detail": detail, "ack_id": a.ID, "acked_by": a.AckedBy, "note": a.Note,
		"quiet_until": quiet, "since_epoch": a.SinceEpoch,
	})
	if err != nil {
		return fmt.Errorf("acknowledgement event: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO events (id, time, received_at, source, kind, server_name, account, stream,
    consumer, client_name, summary, raw)
VALUES (nextval('event_seq'), ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?::JSON)`,
		a.CreatedAt.UTC(), a.CreatedAt.UTC(), SourceFinding, AckEventKind, server, account, stream, consumer, client,
		a.CheckCode+" "+detail, string(raw)); err != nil {
		return fmt.Errorf("acknowledgement event: %w", err)
	}
	return nil
}

// EndAck ends an acknowledgement in force at now as released by by. It
// returns ErrAckNotFound for an unknown ID and ErrAckEnded for one already
// ended or whose episode is over.
func (s *Store) EndAck(ctx context.Context, id int64, by string, now time.Time) (Ack, error) {
	if err := s.lock(ctx, opAcks); err != nil {
		return Ack{}, err
	}
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Ack{}, err
	}
	defer func() { _ = tx.Rollback() }()
	a, err := scanAck(tx.QueryRowContext(ctx, `SELECT `+ackCols+` FROM acknowledgements WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Ack{}, ErrAckNotFound
	}
	if err != nil {
		return Ack{}, err
	}
	var current int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM findings WHERE id = ? AND since_epoch = ? AND state = 'firing'`,
		a.FindingID, a.SinceEpoch).Scan(&current); err != nil {
		return Ack{}, err
	}
	if !a.InForceAt(now) || current == 0 {
		return Ack{}, ErrAckEnded
	}
	if _, err := tx.ExecContext(ctx, `UPDATE acknowledgements SET ended_at = ?, ended_by = ?, end_reason = ? WHERE id = ?`,
		now.UTC(), by, AckReleased, id); err != nil {
		return Ack{}, fmt.Errorf("end acknowledgement %d: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return Ack{}, err
	}
	a.EndedAt, a.EndedBy, a.EndReason = now.UTC(), by, AckReleased
	return a, nil
}

// Acks returns every acknowledgement, oldest first.
func (s *Store) Acks(ctx context.Context) ([]Ack, error) {
	return s.acks(ctx, `true`)
}

func (s *Store) acks(ctx context.Context, where string, args ...any) ([]Ack, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+ackCols+` FROM acknowledgements WHERE `+where+` ORDER BY id`, args...)
	if err != nil {
		return nil, fmt.Errorf("read acknowledgements: %w", err)
	}
	defer rows.Close()
	var out []Ack
	for rows.Next() {
		a, err := scanAck(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// applyAcksAt sets Ack on every firing finding with an acknowledgement of
// its current episode in force at t: the newest, as a new one replaces
// the one before.
func (s *Store) applyAcksAt(ctx context.Context, t time.Time, fs []Finding) error {
	// Compared with the columns directly, so that the parameter is typed
	// as a TIMESTAMP and the session time zone does not shift it.
	all, err := s.acks(ctx, `created_at <= ? AND (ended_at IS NULL OR ended_at > ?)`, t.UTC(), t.UTC())
	if err != nil {
		return err
	}
	byFinding := map[string][]Ack{}
	for _, a := range all {
		byFinding[a.FindingID] = append(byFinding[a.FindingID], a)
	}
	for i := range fs {
		fs[i].Ack = nil
		as := byFinding[fs[i].ID]
		for j := len(as) - 1; j >= 0; j-- {
			if as[j].matches(fs[i]) && as[j].InForceAt(t) {
				a := as[j]
				a.ReadAt = t
				fs[i].Ack = &a
				break
			}
		}
	}
	return nil
}

// sweepAcks deletes the acknowledgements ended before from, and those
// whose episode ended before from: the finding resolved before it, or
// fired again in an episode that began before it, or is gone.
func sweepAcks(ctx context.Context, tx *sql.Tx, from time.Time) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM acknowledgements a WHERE a.ended_at < ?
   OR NOT EXISTS (SELECT 1 FROM findings f WHERE f.id = a.finding_id)
   OR EXISTS (SELECT 1 FROM findings f WHERE f.id = a.finding_id AND (
        (f.since_epoch = a.since_epoch AND f.state = 'resolved' AND f.resolved_at < ?)
     OR (f.since_epoch > a.since_epoch AND f.since < ?)))`, from.UTC(), from.UTC(), from.UTC()); err != nil {
		return fmt.Errorf("sweep acknowledgements: %w", err)
	}
	return nil
}
