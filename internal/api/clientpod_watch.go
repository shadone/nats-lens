package api

import (
	"encoding/json"
	"strings"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// ClientPodWatchPrefix starts the source of a client pod watch's
// collector.k8s_not_watching and collector.k8s_gap events, for example
// "client pods in shop".
const ClientPodWatchPrefix = "client pods "

// ClientPodWatchSQL is true for an event of table alias e that a client
// pod watch recorded. Data history and Replay leave such events out: a
// client pod watch is optional and feeds only Runs on, which the
// Applications page flags itself, so it does not make Kubernetes "not
// watched".
const ClientPodWatchSQL = `coalesce(json_extract_string(e.raw, '$.source'), '') LIKE '` + ClientPodWatchPrefix + `%'`

// IsClientPodWatchEvent reports an event a client pod watch recorded.
func IsClientPodWatchEvent(e *v1.Event) bool {
	if e.GetKind() != "collector.k8s_not_watching" && e.GetKind() != "collector.k8s_gap" {
		return false
	}
	var raw struct {
		Source string `json:"source"`
	}
	_ = json.Unmarshal([]byte(e.GetRaw()), &raw)
	return strings.HasPrefix(raw.Source, ClientPodWatchPrefix)
}
