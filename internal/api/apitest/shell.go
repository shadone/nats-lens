package apitest

import (
	"fmt"

	"google.golang.org/protobuf/proto"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// ShellDiff names the first of the freshness, servers and tally on which a
// GetShellStatus response differs from a GetOverview response of the same
// system and range; "" when they agree. The freshness's age is left out:
// it is measured at the moment of each response.
func ShellDiff(sh *v1.GetShellStatusResponse, o *v1.GetOverviewResponse) string {
	a, b := proto.CloneOf(sh.GetFreshness()), proto.CloneOf(o.GetFreshness())
	if a != nil {
		a.Age = nil
	}
	if b != nil {
		b.Age = nil
	}
	switch {
	case !proto.Equal(a, b):
		return fmt.Sprintf("freshness %v, overview %v", a, b)
	case !proto.Equal(&v1.ListServersResponse{Servers: sh.GetServers()}, &v1.ListServersResponse{Servers: o.GetServers()}):
		return fmt.Sprintf("servers %v, overview %v", sh.GetServers(), o.GetServers())
	case !proto.Equal(sh.GetTally(), o.GetTally()):
		return fmt.Sprintf("tally %v, overview %v", sh.GetTally(), o.GetTally())
	}
	return ""
}
