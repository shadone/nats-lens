package api

import (
	"fmt"
	"math"
	"strings"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// Overview tile IDs, in the order GetOverview returns them.
const (
	TileServers      = "servers"
	TileMetaLeader   = "meta_leader"
	TileStreams      = "streams"
	TileKV           = "kv"
	TileObjectStores = "object_stores"
	TileConsumers    = "consumers"
	TileAccounts     = "accounts"
	TileClients      = "clients"
	TileStorage      = "storage"
)

// TileLabels are the tiles' labels.
var TileLabels = map[string]string{
	TileServers:      "Servers",
	TileMetaLeader:   "Meta leader",
	TileStreams:      "Streams",
	TileKV:           "KV Stores",
	TileObjectStores: "Object Stores",
	TileConsumers:    "Consumers",
	TileAccounts:     "Accounts",
	TileClients:      "Clients",
	TileStorage:      "Storage",
}

// TileDescriptions are the tiles' tooltips: what the number is and how it
// is measured.
var TileDescriptions = map[string]string{
	TileServers: "NATS servers that answered in the newest epoch, out of the servers the system expects; " +
		"the subtitle gives their versions and the shortest uptime.",
	TileMetaLeader: "The server that leads the JetStream meta group, as more than half of the servers that name a leader " +
		"report it in the newest epoch; the number and the chart count leader changes in the range.",
	TileStreams: "Streams any server reports in the newest epoch, without the KV and object store streams; " +
		"replicated means more than one replica, lagging that the leader reports a replica behind.",
	TileKV: "KV buckets, the streams named KV_<bucket>, that any server reports in the newest epoch; " +
		"R1 counts the buckets kept on one server only.",
	TileObjectStores: "Object stores, the streams named OBJ_<bucket>, that any server reports in the newest epoch; " +
		"lagging means the leader reports a replica behind.",
	TileConsumers: "Durable consumers any server reports in the newest epoch; " +
		"behind counts the consumers whose backlog keeps growing (check consumer-002).",
	TileAccounts: "Accounts other than the system account that have a client connection or a stream, " +
		"from the servers' account statistics in the newest epoch.",
	TileClients: "Client connections across all servers as the servers count them, nats-lens's own included; " +
		"apps counts the distinct connection names up to an \"@\", slow the slow consumer events in the range.",
	TileStorage: "JetStream file storage in use across all servers, every replica counted; the limit is the sum of " +
		"the servers' storage limits and the growth a robust slope over the range, per day.",
}

// TileRule says which findings concern a tile: those whose check code is
// one of Codes, or starts with one ending in "-", and whose entity Entity
// accepts (all when nil). Types are the entity types whose checks must
// have run for a tile without a firing finding to be ok. A tile without a
// rule has no check, and its status is no data.
type TileRule struct {
	Codes  []string
	Types  []string
	Entity func(*v1.EntityRef) bool
}

// streamKind accepts the findings of streams whose name has the prefix,
// or of plain streams (not KV_ or OBJ_) for an empty prefix.
func streamKind(prefix string) func(*v1.EntityRef) bool {
	return func(e *v1.EntityRef) bool {
		name := e.GetStream()
		if prefix == "" {
			return !strings.HasPrefix(name, "KV_") && !strings.HasPrefix(name, "OBJ_")
		}
		return strings.HasPrefix(name, prefix)
	}
}

// TileRules are the tiles' rules.
var TileRules = map[string]TileRule{
	TileServers: {Codes: []string{"server-001", "server-002", "server-003", "server-004", "server-005", "server-006", "system-"},
		Types: []string{"server", "system"}},
	TileMetaLeader:   {Codes: []string{"system-002", "system-003"}, Types: []string{"system"}},
	TileStreams:      {Codes: []string{"stream-"}, Types: []string{"stream"}, Entity: streamKind("")},
	TileKV:           {Codes: []string{"stream-"}, Types: []string{"stream"}, Entity: streamKind("KV_")},
	TileObjectStores: {Codes: []string{"stream-"}, Types: []string{"stream"}, Entity: streamKind("OBJ_")},
	TileConsumers:    {Codes: []string{"consumer-"}, Types: []string{"consumer"}},
	TileClients: {Codes: []string{"client-", "server-011", "server-015", "server-009", "server-010"},
		Types: []string{"client", "server"}},
	TileStorage: {Codes: []string{"server-007", "server-014", "stream-004", "stream-006"}, Types: []string{"server", "stream"}},
}

// Concerns reports whether the finding is one of the rule's.
func (r TileRule) Concerns(f *v1.Finding) bool {
	if r.Entity != nil && !r.Entity(f.GetEntity()) {
		return false
	}
	for _, c := range r.Codes {
		if f.GetCheckCode() == c || (strings.HasSuffix(c, "-") && strings.HasPrefix(f.GetCheckCode(), c)) {
			return true
		}
	}
	return false
}

// ServersSubtitle names the versions and the shortest uptime of the
// servers that answered.
func ServersSubtitle(servers []*v1.ServerSummary) string {
	versions := map[string]bool{}
	var up time.Duration
	seen := false
	for _, s := range servers {
		if s.GetVersion() != "" {
			versions[s.GetVersion()] = true
		}
		if s.GetPresent() && s.GetUptime() != nil {
			if d := s.GetUptime().AsDuration(); !seen || d < up {
				up, seen = d, true
			}
		}
	}
	var parts []string
	switch {
	case len(versions) == 1:
		for v := range versions {
			parts = append(parts, "v"+v+" on all")
		}
	case len(versions) > 1:
		parts = append(parts, fmt.Sprintf("%d versions", len(versions)))
	}
	if seen {
		parts = append(parts, FmtUptime(up))
	}
	return strings.Join(parts, " · ")
}

// FmtBytes writes a byte count in binary units, for example "17 MiB" or
// "1.2 GiB".
func FmtBytes(b float64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB"}
	i := 0
	for math.Abs(b) >= 1024 && i < len(units)-1 {
		b /= 1024
		i++
	}
	if i > 0 && math.Abs(b) < 10 {
		return fmt.Sprintf("%.1f %s", b, units[i])
	}
	return fmt.Sprintf("%.0f %s", b, units[i])
}

// FmtGrowth writes a growth per day with its sign, for example
// "+17 MiB/day".
func FmtGrowth(perDay float64) string {
	sign := "+"
	if perDay < 0 {
		sign, perDay = "-", -perDay
	}
	return sign + FmtBytes(perDay) + "/day"
}

// FmtUptime writes an uptime for a tile: minutes under an hour, hours
// under three days, days beyond.
func FmtUptime(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("up %d min", int(d.Minutes()))
	case d < 72*time.Hour:
		return fmt.Sprintf("up %d h", int(d.Hours()))
	}
	return fmt.Sprintf("up %d d", int(d.Hours()/24))
}
