package archive

import (
	"context"
	"fmt"
	"os"
	"path"
	"time"

	"github.com/shadone/nats-lens/internal/store"
)

// pruneEvery is how often Export looks for hours past ArchiveRetention.
const pruneEvery = time.Hour

// prune removes every archived hour that ended more than ArchiveRetention
// before now: first its data files of every export, then its manifest, so
// that an interrupted prune leaves a manifest the next prune finds again.
// Such an hour is older than the database retention, so no rebuild loads
// it. A zero ArchiveRetention keeps every hour.
func (a *Archiver) prune(ctx context.Context, now time.Time) error {
	if a.ArchiveRetention <= 0 || (!a.lastPrune.IsZero() && now.Sub(a.lastPrune) < pruneEvery) {
		return nil
	}
	rels, err := a.Dest.List(ctx, manifestDir+"/")
	if err != nil {
		return fmt.Errorf("list archive manifests: %w", err)
	}
	cutoff := now.Add(-a.ArchiveRetention)
	pruned := 0
	for _, rel := range rels {
		hour, ok := parseManifestRelPath(rel)
		if !ok || hour.Add(time.Hour).After(cutoff) {
			continue
		}
		for _, table := range store.ArchiveTableNames() {
			files, err := a.Dest.List(ctx, hourPrefix(table, hour))
			if err != nil {
				return fmt.Errorf("list archive files of %s: %w", rel, err)
			}
			for _, f := range files {
				if err := a.Dest.Remove(ctx, f); err != nil {
					return fmt.Errorf("prune %s: %w", f, err)
				}
			}
			removeEmptyDir(a.Dest, path.Dir(hourPrefix(table, hour)))
		}
		if err := a.Dest.Remove(ctx, rel); err != nil {
			return fmt.Errorf("prune %s: %w", rel, err)
		}
		removeEmptyDir(a.Dest, path.Dir(rel))
		pruned++
	}
	a.lastPrune = now
	if pruned > 0 {
		a.Log.Info("archive hours pruned", "hours", pruned, "older_than", cutoff.UTC())
	}
	return nil
}

// removeEmptyDir removes the directory rel of a local archive once it holds
// no file, so that pruned days leave no empty directories behind. Only
// directories of pruned hours are passed, which no export writes into.
func removeEmptyDir(d Destination, rel string) {
	if l, ok := d.(LocalDir); ok {
		if p, err := l.local(rel); err == nil && rel != "." {
			_ = os.Remove(p) // fails while the directory holds files
		}
	}
}
