package storeprov

import (
	"context"
	"slices"
	"strings"

	"github.com/shadone/nats-lens/internal/checks"
)

// The tooling rule of the client checks, for the pages that leave
// nats-lens's own connections out: a connection is tooling when its name is
// one of the tooling names, or when it comes from a loopback address while
// loopback counts as tooling (checks.loopback_is_tooling and the guard's
// decision stored with the epoch; an epoch without one counts as on).
// checks.include_tooling does not apply: hiding tooling only changes what
// is shown.

// loopbackAddrs are the client addresses of a loopback connection.
var loopbackAddrs = []string{"127.0.0.1", "::1"}

// toolingRule is the rule as it holds for one epoch.
type toolingRule struct {
	names []string
	// loopback is whether a loopback address makes a connection tooling.
	loopback bool
}

// toolingAt reads the rule for epoch.
func (p *Provider) toolingAt(ctx context.Context, epoch int64) (toolingRule, error) {
	stored, err := checks.StoredLoopback(ctx, p.db(), epoch)
	if err != nil {
		return toolingRule{}, err
	}
	return toolingRule{names: append([]string{}, p.ToolingNames...), loopback: !p.LoopbackNotTooling && stored}, nil
}

// is reports whether the connection named name from address ip is tooling.
func (r toolingRule) is(name, ip string) bool {
	return slices.Contains(r.names, name) || (r.loopback && slices.Contains(loopbackAddrs, ip))
}

// sql is the rule as an SQL condition on the columns name and ip, with its
// arguments.
func (r toolingRule) sql(name, ip string) (string, []any) {
	return toolingSQL(name, ip, "?"), []any{r.names, r.loopback}
}

// eventToolingSQL is the rule for an event whose epoch (the newest that
// started at or before it) is joined as x, on the columns name and host,
// with its arguments: the loopback decision is each event's epoch's.
func (p *Provider) eventToolingSQL(name, host string) (string, []any) {
	return toolingSQL(name, host, "? AND coalesce(x.loopback_tooling, true)"),
		[]any{append([]string{}, p.ToolingNames...), !p.LoopbackNotTooling}
}

// toolingSQL is the condition "name is a tooling name, or ip is a loopback
// address while loopback holds". Its first placeholder takes the tooling
// names as a list; loopback is an SQL condition whose placeholders follow.
func toolingSQL(name, ip, loopback string) string {
	return `(list_contains(?::VARCHAR[], coalesce(` + name + `, '')) OR ((` + loopback + `) AND coalesce(` + ip +
		`, '') IN ('` + strings.Join(loopbackAddrs, `', '`) + `')))`
}
