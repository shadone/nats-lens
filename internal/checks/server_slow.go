//go:build !(js && wasm)

// Left out of WebAssembly builds because it reads the store; see engine.go.

package checks

import (
	"context"
	"database/sql"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/shadone/nats-lens/internal/routehealth"
	"github.com/shadone/nats-lens/internal/store"
)

// slowRoutes uses three bounded collection reads (current rows, previous
// collection selection, previous rows), independent of the server count.
func (e *Engine) slowRoutes(ctx context.Context, conn *sql.Conn, epoch int64, at time.Time) ([]store.FindingRow, error) {
	rows, err := routehealth.ReadRows(ctx, conn, epoch)
	if err != nil {
		return nil, err
	}
	earlier, err := routehealth.ReadEarlier(ctx, conn, epoch, at, e.Interval)
	if err != nil {
		return nil, err
	}
	routes, err := routehealth.RoutesContext(ctx, rows, earlier, at, e.Interval, nil, nil)
	if err != nil {
		return nil, err
	}
	peers := map[string][]string{}
	for _, r := range routes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !r.GetSlow() {
			continue
		}
		a, b := r.GetServerA(), r.GetServerB()
		peers[a] = append(peers[a], fmt.Sprintf("%s (%s)", b, r.GetRtt().AsDuration()))
		peers[b] = append(peers[b], fmt.Sprintf("%s (%s)", a, r.GetRtt().AsDuration()))
	}
	var out []store.FindingRow
	for _, name := range slices.Sorted(maps.Keys(peers)) {
		ps := peers[name]
		slices.Sort(ps)
		value := float64(len(ps))
		out = append(out, store.FindingRow{EntityKey: name, Severity: "warning", Value: &value,
			Detail: fmt.Sprintf("%s: slow route pairs in two eligible collections (collections %d and %d): %s. Both ends are listed; this does not identify the responsible server.", name, earlier.Epoch, epoch, strings.Join(ps, ", "))})
	}
	return out, ctx.Err()
}
