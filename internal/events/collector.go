package events

import (
	"encoding/json"
	"fmt"
	"time"
)

// StartEvents returns the events the collector records when it starts: a
// collector.gap covering the time since last, the latest epoch or event
// stored, so that the missing capture is never read as a quiet system, and
// a collector.started marker. last is zero for an empty database, which has
// no gap to record.
func StartEvents(last, now time.Time, version string) []Event {
	now = now.UTC()
	var out []Event
	if !last.IsZero() && now.After(last) {
		last = last.UTC()
		gap := now.Sub(last).Round(time.Second)
		out = append(out, Event{
			Time: last, ReceivedAt: now, Source: SourceCollector, Kind: "collector.gap",
			Reason:  "collector not running",
			Summary: fmt.Sprintf("no capture for %s, from %s to %s", gap, last.Format(time.RFC3339), now.Format(time.RFC3339)),
			Raw: mustJSON(map[string]any{
				"from": last, "to": now, "seconds": int64(gap / time.Second),
			}),
		})
	}
	out = append(out, Event{
		Time: now, ReceivedAt: now, Source: SourceCollector, Kind: "collector.started",
		Summary: "collector started, version " + version,
		Raw:     mustJSON(map[string]string{"version": version}),
	})
	return out
}

// mustJSON marshals values that cannot fail to marshal (maps of strings,
// numbers and times).
func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
