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

// SlowRouteCode is the built-in check sharing Topology's classifier.
const SlowRouteCode = "server-025"

func slowRouteCheck() *Check {
	return &Check{
		Code: SlowRouteCode, Name: "Slow routes", Entity: "server", Category: "health", Severity: "warning", Kind: "operational",
		Description: "One or more of the server's route pairs are slow by Topology's rule in two eligible collections. Both ends receive a finding; a slow link does not establish which end is responsible. The previous route-bearing collection must be within three collect intervals. An unknown interval or unreported RTT cannot establish slowness.",
		Remediation: "Read the slow pairs on the Topology page. Compare the named peers' network latency, packet loss, CPU and scheduling pressure with the cluster's other servers. Check zone placement and network paths before changing configuration. nats-lens only observes the routes.",
		ValueName:   "slow route pairs", ValueBasis: "pairs touching this server classified slow in both eligible collections by Topology's shared rule",
		builtin: true,
	}
}

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
