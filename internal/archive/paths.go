package archive

import (
	"fmt"
	"strings"
	"time"
)

// manifestDir holds the per-hour manifests.
const manifestDir = "manifests"

// StateRelPath is the archive path of the state object: the highest epoch
// number and event ID when it was written.
const StateRelPath = "state.json"

// ExportID names one export of an hour: its UTC start time to the
// nanosecond. Every export writes new file names, so a crash during a
// re-export leaves the files of the previous manifest in place.
func ExportID(t time.Time) string {
	return t.UTC().Format("20060102T150405.000000000Z")
}

// RelPath returns the archive path of a table's file for an hour, written
// by the export id names.
func RelPath(table string, hour time.Time, id string) string {
	return hourPrefix(table, hour) + id + ".parquet"
}

// hourPrefix is the path prefix of every file of a table for an hour.
func hourPrefix(table string, hour time.Time) string {
	h := hour.UTC()
	return fmt.Sprintf("%s/date=%s/hour=%02d.", table, h.Format("2006-01-02"), h.Hour())
}

// ManifestRelPath returns the archive path of an hour's manifest, which
// lists the hour's files and is written after them.
func ManifestRelPath(hour time.Time) string {
	h := hour.UTC()
	return fmt.Sprintf("%s/date=%s/hour=%02d.json", manifestDir, h.Format("2006-01-02"), h.Hour())
}

// parseManifestRelPath reads the hour from a manifest path.
func parseManifestRelPath(rel string) (time.Time, bool) {
	parts := strings.Split(rel, "/")
	if len(parts) != 3 || parts[0] != manifestDir {
		return time.Time{}, false
	}
	date, ok1 := strings.CutPrefix(parts[1], "date=")
	hh, ok2 := strings.CutPrefix(parts[2], "hour=")
	hh, ok3 := strings.CutSuffix(hh, ".json")
	if !ok1 || !ok2 || !ok3 {
		return time.Time{}, false
	}
	t, err := time.Parse("2006-01-02 15", date+" "+hh)
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}

// ParseRelPath reads the table, hour and export id from a data file path;
// ok is false for any other file. The id is empty for files written before
// export ids (hour=HH.parquet).
func ParseRelPath(rel string) (table string, hour time.Time, id string, ok bool) {
	parts := strings.Split(rel, "/")
	if len(parts) != 3 {
		return "", time.Time{}, "", false
	}
	date, ok1 := strings.CutPrefix(parts[1], "date=")
	rest, ok2 := strings.CutPrefix(parts[2], "hour=")
	rest, ok3 := strings.CutSuffix(rest, ".parquet")
	if !ok1 || !ok2 || !ok3 {
		return "", time.Time{}, "", false
	}
	hh, id, _ := strings.Cut(rest, ".")
	t, err := time.Parse("2006-01-02 15", date+" "+hh)
	if err != nil || len(hh) != 2 {
		return "", time.Time{}, "", false
	}
	return parts[0], t.UTC(), id, true
}
