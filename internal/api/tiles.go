package api

import (
	"fmt"
	"math"
	"time"
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
