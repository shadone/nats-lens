package storeprov

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// samplesMissing reports whether an epoch from the first epoch starting
// at or after the first parameter to the last starting before the second
// has no stream samples. A sample's previous sample lies in that span
// whenever the series of the range can compare with it, so a range whose
// epochs all have samples reads the same from stream_samples as from the
// reports.
const samplesMissing = `WITH r AS (SELECT min(epoch) AS lo, max(epoch) AS hi FROM epochs WHERE started_at >= ? AND started_at < ?)
SELECT EXISTS (SELECT 1 FROM epochs e WHERE e.epoch BETWEEN (SELECT lo FROM r) AND (SELECT hi FROM r)
  AND e.epoch NOT IN (SELECT epoch FROM stream_sample_epochs))`

// streamSamplesCover reports whether every epoch of the range [start, end)
// has its stream samples.
func (p *Provider) streamSamplesCover(ctx context.Context, start, end time.Time) (bool, error) {
	var missing bool
	if err := p.db().QueryRowContext(ctx, samplesMissing, start, end).Scan(&missing); err != nil {
		return false, fmt.Errorf("stream samples: %w", err)
	}
	return !missing, nil
}

// streamPerEpochSQL selects account, stream, t and the named values of
// every stream in each epoch starting in [?, ?) whose account and stream
// match the next four parameters (empty for any), and filter (an AND clause
// on x, with its own parameters): from stream_samples, or with samples
// false from the reports, the leader's value else the largest. cols pairs
// a stream_samples column with its alias.
func streamPerEpochSQL(samples bool, filter string, cols ...[2]string) string {
	sel := make([]string, len(cols))
	for i, c := range cols {
		if samples {
			sel[i] = "x." + c[0] + " AS " + c[1]
		} else {
			sel[i] = leaderValue(c[0]) + " AS " + c[1]
		}
	}
	where := `WHERE e.started_at >= ? AND e.started_at < ? AND (? = '' OR x.account = ?) AND (? = '' OR x.stream = ?)` + filter
	if samples {
		return `SELECT x.account, x.stream, e.started_at AS t, ` + strings.Join(sel, ", ") + `
  FROM stream_samples x JOIN epochs e USING (epoch)
  ` + where
	}
	return `SELECT x.account, x.stream, e.started_at AS t, ` + strings.Join(sel, ", ") + `
  FROM stream_stats x JOIN epochs e USING (epoch)
  ` + where + `
  GROUP BY x.account, x.stream, e.epoch, e.started_at`
}

// sampleRate is stream.msgs_rate from stream_samples: the per-second
// advance of seq from the stream's previous sample, by the rules of
// resourceSequenceRate. The previous sample counts only when it lies in
// the epochs the reports' window reads (from the first epoch of the
// lookback on), where the reports' lag finds the same sample. The
// parameters are those of resourceSequenceRate: the lookback start, the
// range's end, then where's.
func sampleRate(where string) string {
	return `WITH sequence_epochs AS (
 SELECT epoch FROM epochs WHERE started_at>=? AND started_at<?)
 SELECT e.started_at AS t,(x.seq::HUGEINT-x.prev_seq::HUGEINT)*1000.0/date_diff('millisecond',pe.started_at,e.started_at) AS v
 FROM stream_samples x JOIN epochs e USING(epoch) JOIN epochs pe ON pe.epoch=x.prev_epoch
 WHERE x.epoch IN(SELECT epoch FROM sequence_epochs) ` + where + `
 AND x.prev_epoch>=(SELECT min(epoch) FROM sequence_epochs)
 AND x.created IS NOT NULL AND x.prev_created=x.created AND x.seq>=0 AND x.prev_seq>=0 AND x.seq>=x.prev_seq
 AND date_diff('millisecond',pe.started_at,e.started_at)>0 AND date_diff('millisecond',pe.started_at,e.started_at)<=` + maxGapToken
}

// sampleValue reads one stream_samples column per epoch for the stream
// where selects, with the parameters of leaderView.
func sampleValue(col, where string) string {
	return fmt.Sprintf(`
SELECT e.started_at AS t, x.%[1]s AS v
FROM stream_samples x JOIN epochs e USING (epoch)
WHERE e.started_at >= ? AND e.started_at < ? %[2]s`, col, where)
}
