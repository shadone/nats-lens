// Package investigation defines the shared read-only investigation contract.
// It contains no database queries or metric calculations.
package investigation

import (
	"slices"
	"strings"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/reflect/protoreflect"
)

const Path = "/api/read-only"
const Service = "natslens.v1.LensService"

// Explicit admission means a newly added RPC is unavailable until reviewed.
// ASVS 8.3.1: the HTTP service uses the same allowlist at its trusted boundary.
var methods = []string{
	"GetLensInfo", "ListSystems", "ListMetrics", "ListCheckDefinitions",
	"GetOverview", "GetShellStatus", "GetTimeSeries", "GetTimeSeriesBatch",
	"ListFindings", "GetFinding", "GetOnCall", "ListEvents", "GetEvent",
	"GetStatusTimeline", "ListServers", "GetServer", "ListAccounts",
	"ListConnections", "ListStreams", "GetStream", "ListConsumers", "GetConsumer",
	"ListKeyValueStores", "ListObjectStores", "GetCapacity", "GetDataHistory",
	"GetPlacement", "GetReplay", "GetTopology", "GetAccountsAndAuth",
	"GetSubjects", "MatchSubject", "GetMicroServices", "GetApplications",
	"GetSlowConsumerInvestigation", "GetStorageChange",
}

func Methods() []string { return slices.Clone(methods) }
func Method(name string) protoreflect.MethodDescriptor {
	if !slices.Contains(methods, name) {
		return nil
	}
	return v1.File_natslens_v1_service_proto.Services().ByName("LensService").Methods().ByName(protoreflect.Name(name))
}
func AllowedProcedure(path string) bool {
	prefix := "/" + Service + "/"
	return strings.HasPrefix(path, prefix) && Method(strings.TrimPrefix(path, prefix)) != nil
}
