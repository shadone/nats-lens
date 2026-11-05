package archive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/shadone/nats-lens/internal/store"
)

// DefaultMinCleanupAge is how old an unlisted file must be before an
// export removes it, so that an export still running in another process
// keeps its files until its manifest lists them.
const DefaultMinCleanupAge = 10 * time.Minute

// DefaultMaxHoursPerTick bounds the hours one export tick writes, so that
// catching up after a long downtime is spread over several ticks.
const DefaultMaxHoursPerTick = 6

// Archiver exports hours and rebuilds a database from the archive.
type Archiver struct {
	Store *store.Store
	Dest  Destination
	// Retention bounds both: hours older than it are neither exported
	// (the sweep may have removed part of them) nor loaded.
	Retention time.Duration
	// Grace is how long after an hour ends it is still open: an epoch that
	// started in it may still be writing, and events wait for their batch.
	Grace time.Duration
	// ProvisionalEvery is how often the open hour is exported as it is so
	// far, so that a lost database loses at most this much; 0 disables it.
	ProvisionalEvery time.Duration
	// MaxHoursPerTick bounds the hours one call to Export writes; 0 means
	// no bound.
	MaxHoursPerTick int
	// MinCleanupAge is how old an unlisted file of an hour must be before
	// an export removes it; see removeUnlisted. Zero removes at once.
	MinCleanupAge time.Duration
	// ArchiveRetention is how long archived hours are kept: after an
	// export, hours that ended longer ago are removed. 0 keeps them all. It
	// must not be shorter than Retention, or a rebuild would miss hours.
	ArchiveRetention time.Duration
	// Clock gives the time export ids are made from; nil is time.Now.
	Clock func() time.Time
	Log   *slog.Logger

	lastPrune time.Time
	status    status
}

// Manifest lists the files of one exported hour. It is written after the
// files, so a rebuild loads only hours whose export completed.
type Manifest struct {
	Hour       time.Time      `json:"hour"`
	Final      bool           `json:"final"`
	ExportedAt time.Time      `json:"exported_at"`
	Epochs     int64          `json:"epochs"`
	Events     int64          `json:"events"`
	Files      []ManifestFile `json:"files"`
}

// ManifestFile is one table's file for the hour.
type ManifestFile struct {
	Table string `json:"table"`
	Path  string `json:"path"`
	Rows  int64  `json:"rows"`
}

// State records the highest epoch number, event ID, mute ID and
// acknowledgement ID (0 in a state written before acknowledgements) at an
// export tick and at shutdown, so that a rebuild never hands out a number
// the lost database had already used before then.
type State struct {
	MaxEpoch   int64     `json:"max_epoch"`
	MaxEventID int64     `json:"max_event_id"`
	MaxMuteID  int64     `json:"max_mute_id"`
	MaxAckID   int64     `json:"max_ack_id"`
	WrittenAt  time.Time `json:"written_at"`
}

// Run calls Tick every interval until ctx is done. The caller writes the
// last state with WriteState once the last events are stored.
func (a *Archiver) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if err := a.Tick(ctx, time.Now()); err != nil && ctx.Err() == nil {
			a.Log.Error("archive export failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Tick exports the pending hours and writes the state object.
func (a *Archiver) Tick(ctx context.Context, now time.Time) error {
	_, err := a.Export(ctx, now)
	err = errors.Join(err, a.WriteState(ctx, now))
	a.status.failed(err, now)
	return err
}

// Export exports pending hours inside the retention window, oldest first,
// at most MaxHoursPerTick of them, and returns how many it exported: closed
// hours not exported yet or whose rows changed since, and the open hour
// provisionally. Re-exporting an hour overwrites its files. After a
// successful export it lists the manifests, at most once an hour, and
// prunes hours past ArchiveRetention.
func (a *Archiver) Export(ctx context.Context, now time.Time) (int, error) {
	closedBefore := now.Add(-a.Grace).UTC().Truncate(time.Hour)
	from := now.Add(-a.Retention).UTC().Truncate(time.Hour).Add(time.Hour)
	hours, err := a.Store.PendingHours(ctx, from, closedBefore, now, a.ProvisionalEvery)
	if err != nil {
		return 0, err
	}
	if a.MaxHoursPerTick > 0 && len(hours) > a.MaxHoursPerTick {
		hours = hours[:a.MaxHoursPerTick]
	}
	for i, h := range hours {
		if err := a.ExportHour(ctx, h.Hour, h.Final, now); err != nil {
			return i, err
		}
	}
	return len(hours), a.prune(ctx, now)
}

// ExportHour writes one hour's files under new names, then its manifest,
// records the hour as exported, and removes the hour's files the manifest
// does not list (those of earlier exports, or of one that did not finish).
// Until the manifest is replaced, the previous manifest and its files stay
// as they were, so a crash never mixes two exports.
func (a *Archiver) ExportHour(ctx context.Context, hour time.Time, final bool, now time.Time) error {
	stage := a.Dest.StageDir()
	if err := os.MkdirAll(stage, 0o750); err != nil {
		return fmt.Errorf("stage dir: %w", err)
	}
	id, err := a.newExportID(ctx, hour)
	if err != nil {
		return err
	}
	tag := hour.UTC().Format("20060102T15")
	exp, err := a.Store.ExportHour(ctx, hour, func(table string) string {
		return filepath.Join(stage, table+"."+tag+"."+id+".parquet.tmp")
	})
	if err != nil {
		removeStaged(exp.Files)
		return fmt.Errorf("export %s: %w", tag, err)
	}
	m := Manifest{Hour: hour.UTC(), Final: final, ExportedAt: now.UTC(), Epochs: exp.Epochs, Events: exp.Events}
	for i, f := range exp.Files {
		rel := RelPath(f.Table, hour, id)
		if err := a.Dest.Put(ctx, rel, f.Path); err != nil {
			removeStaged(exp.Files[i:])
			return err
		}
		m.Files = append(m.Files, ManifestFile{Table: f.Table, Path: rel, Rows: f.Rows})
	}
	if err := a.putJSON(ctx, ManifestRelPath(hour), m); err != nil {
		return err
	}
	if err := a.Store.MarkExported(ctx, hour, exp, final, now); err != nil {
		return fmt.Errorf("mark %s exported: %w", tag, err)
	}
	a.status.exported(hour, final, now)
	a.removeUnlisted(ctx, hour, m, id)
	a.Log.Info("hour archived", "hour", hour.UTC(), "final", final, "files", len(exp.Files), "rows", exp.Rows())
	return nil
}

// newExportID returns an export id for hour that no file of the hour
// carries yet: a clock that repeats a time, or one set back, never makes
// two exports share file names.
func (a *Archiver) newExportID(ctx context.Context, hour time.Time) (string, error) {
	clock := a.Clock
	if clock == nil {
		clock = time.Now
	}
	used := map[string]bool{}
	for _, table := range store.ArchiveTableNames() {
		rels, err := a.Dest.List(ctx, hourPrefix(table, hour))
		if err != nil {
			return "", fmt.Errorf("list archive files: %w", err)
		}
		for _, rel := range rels {
			if _, _, id, ok := ParseRelPath(rel); ok {
				used[id] = true
			}
		}
	}
	t := clock()
	for used[ExportID(t)] {
		t = t.Add(time.Nanosecond)
	}
	return ExportID(t), nil
}

// removeUnlisted deletes the hour's data files that m, written by export
// id, does not list: files of earlier exports (a lower id, or none for
// files written before export ids) that are older than MinCleanupAge.
// Another process exporting the same hour at the same time writes files
// its manifest will list soon; the age rule keeps them. A failure is
// logged: the files are only unused, and a later export tries again.
func (a *Archiver) removeUnlisted(ctx context.Context, hour time.Time, m Manifest, id string) {
	keep := map[string]bool{}
	for _, f := range m.Files {
		keep[f.Path] = true
	}
	for _, table := range store.ArchiveTableNames() {
		rels, err := a.Dest.List(ctx, hourPrefix(table, hour))
		if err != nil {
			a.Log.Warn("archive files not listed for cleanup", "table", table, "hour", hour.UTC(), "err", err)
			continue
		}
		for _, rel := range rels {
			if keep[rel] {
				continue
			}
			if _, _, fid, ok := ParseRelPath(rel); !ok || (fid != "" && fid >= id) {
				continue
			}
			if a.MinCleanupAge > 0 {
				mod, err := a.Dest.ModTime(ctx, rel)
				if err != nil || time.Since(mod) < a.MinCleanupAge {
					continue
				}
			}
			if err := a.Dest.Remove(ctx, rel); err != nil {
				a.Log.Warn("superseded archive file not removed", "path", rel, "err", err)
			}
		}
	}
}

// WriteState records the highest epoch number, event ID, mute ID and
// acknowledgement ID.
func (a *Archiver) WriteState(ctx context.Context, now time.Time) error {
	ids, err := a.Store.MaxIDs(ctx)
	if err != nil {
		return fmt.Errorf("read highest IDs: %w", err)
	}
	return a.putJSON(ctx, StateRelPath, State{MaxEpoch: ids.Epoch, MaxEventID: ids.Event, MaxMuteID: ids.Mute, MaxAckID: ids.Ack, WrittenAt: now.UTC()})
}

// putJSON stages v as JSON and puts it at rel.
func (a *Archiver) putJSON(ctx context.Context, rel string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	stage := a.Dest.StageDir()
	if err := os.MkdirAll(stage, 0o750); err != nil {
		return fmt.Errorf("stage dir: %w", err)
	}
	f, err := os.CreateTemp(stage, "*.json.tmp")
	if err != nil {
		return err
	}
	_, werr := f.Write(append(b, '\n'))
	if err := errors.Join(werr, f.Close()); err != nil {
		_ = os.Remove(f.Name())
		return err
	}
	if err := a.Dest.Put(ctx, rel, f.Name()); err != nil {
		_ = os.Remove(f.Name())
		return err
	}
	return nil
}

func removeStaged(files []store.ExportedFile) {
	for _, f := range files {
		_ = os.Remove(f.Path)
	}
}

// ErrEmptyArchive is returned by Rebuild when the archive holds no hour
// with a manifest inside the retention window.
var ErrEmptyArchive = errors.New("archive has no complete hour inside the retention window")

// Rebuild loads every archived hour inside the retention window that has a
// manifest into the store, which must be empty: exactly the files the
// manifest lists, each checked against the row count it lists. Files no
// manifest lists are an export that did not finish and are skipped.
func (a *Archiver) Rebuild(ctx context.Context, now time.Time) error {
	rels, err := a.Dest.List(ctx, manifestDir+"/")
	if err != nil {
		return fmt.Errorf("list archive: %w", err)
	}
	from := now.Add(-a.Retention).UTC().Truncate(time.Hour)
	known := store.ArchiveTableNames()
	hours := map[time.Time]store.RebuildHour{}
	for _, rel := range rels {
		hour, ok := parseManifestRelPath(rel)
		if !ok || hour.Before(from) {
			continue
		}
		var m Manifest
		if err := a.readJSON(ctx, rel, &m); err != nil {
			return fmt.Errorf("manifest %s: %w", rel, err)
		}
		rh := store.RebuildHour{Files: map[string]string{}, Final: m.Final}
		for _, f := range m.Files {
			if !slices.Contains(known, f.Table) {
				continue
			}
			p, err := a.Dest.Fetch(ctx, f.Path)
			if err != nil {
				return err
			}
			// A file that does not hold the rows its manifest lists is
			// damaged; loading it would silently change history.
			rows, err := a.Store.ParquetRows(ctx, p)
			if err != nil {
				return fmt.Errorf("manifest %s: file %s: %w (remove the manifest to rebuild without this hour)", rel, f.Path, err)
			}
			if rows != f.Rows {
				return fmt.Errorf("manifest %s: file %s holds %d rows, the manifest lists %d (remove the manifest to rebuild without this hour)",
					rel, f.Path, rows, f.Rows)
			}
			rh.Files[f.Table] = p
		}
		hours[hour] = rh
	}
	if len(hours) == 0 {
		return ErrEmptyArchive
	}
	var st State
	if err := a.readJSON(ctx, StateRelPath, &st); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("state: %w", err)
	}
	if err := a.Store.Rebuild(ctx, hours, store.MaxIDs{Epoch: st.MaxEpoch, Event: st.MaxEventID, Mute: st.MaxMuteID, Ack: st.MaxAckID}, now); err != nil {
		return err
	}
	a.Log.Info("database rebuilt from archive", "hours", len(hours), "since", from,
		"state_max_epoch", st.MaxEpoch, "state_max_event_id", st.MaxEventID, "state_max_mute_id", st.MaxMuteID)
	return nil
}

func (a *Archiver) readJSON(ctx context.Context, rel string, v any) error {
	p, err := a.Dest.Fetch(ctx, rel)
	if err != nil {
		return err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}
