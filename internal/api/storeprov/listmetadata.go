package storeprov

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/shadone/nats-lens/internal/api"
)

// Proposed, not yet confirmed: each auxiliary metadata read family has a
// 100,000-row / 8 MiB encoded projection budget. SQL execution/aggregation is
// outside this application read bound. Overflow fails the whole capture.
const maxListMetadataRows = 100000
const maxListMetadataBytes = 8 << 20

type metadataBudget struct{ rows, bytes int }

func newMetadataBudget() *metadataBudget {
	return &metadataBudget{maxListMetadataRows, maxListMetadataBytes}
}
func metadataOverflow() error {
	return fmt.Errorf("%w: list metadata budget exceeded; narrow the account or stream filter", api.ErrResourceExhausted)
}

// readMetadata bounds database output AND scanner work. The overflow row is
// never scanned. Oversized payloads are replaced in SQL by an empty sentinel;
// the scanner rejects their original byte size before decoding or retaining.
// q projects only the metadata used by its caller; all values remain parameters.
func (p *Provider) readMetadata(ctx context.Context, q string, args []any, budget *metadataBudget, visit func([]byte) error) error {
	rows, err := p.db().QueryContext(ctx, `WITH metadata AS (`+q+`), encoded AS (
 SELECT to_json(metadata)::VARCHAR AS payload FROM metadata
 ), sized AS (SELECT payload, octet_length(encode(payload)) AS bytes FROM encoded),
 cumulative AS (SELECT payload, bytes, sum(bytes) OVER (ROWS UNBOUNDED PRECEDING) AS read_bytes FROM sized)
 SELECT CASE WHEN read_bytes <= ? THEN payload ELSE '' END, bytes FROM cumulative
 WHERE read_bytes - bytes <= ? LIMIT ?`,
		append(args, budget.bytes, budget.bytes, budget.rows+1)...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if budget.rows == 0 {
			return metadataOverflow()
		}
		var raw string
		var size int
		if err := rows.Scan(&raw, &size); err != nil {
			return err
		}
		if size > budget.bytes {
			return metadataOverflow()
		}
		budget.rows--
		budget.bytes -= size
		if err := visit([]byte(raw)); err != nil {
			return err
		}
	}
	return rows.Err()
}

// AccountNamesFor is the optional page-decoration path. It only reads accounts
// present on the selected page and does not build the whole account directory.
func (p *Provider) AccountNamesFor(ctx context.Context, accounts []string) (map[string]string, error) {
	out := map[string]string{}
	err := p.readMetadata(ctx, `SELECT account, account_name FROM accounts WHERE account IN (SELECT unnest(?::VARCHAR[]))
 AND account_name IS NOT NULL AND account_name <> account`, []any{accounts}, newMetadataBudget(), func(raw []byte) error {
		var v struct {
			Account string
			Name    string `json:"account_name"`
		}
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		out[v.Account] = v.Name
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
