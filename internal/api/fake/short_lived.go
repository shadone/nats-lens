package fake

import (
	"fmt"
	"strings"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// shortLived is the client-005 finding of a name whose churn summaries in
// the 10 minutes before now count connects, from hosts on servers, as the
// check's query writes it.
func shortLived(name string, since time.Time, connects int, hosts, servers []string) *v1.Finding {
	f := finding("client-005", "Short-lived connections", warning, firing, clientRef(name), since, since,
		fmt.Sprintf("%d connections closed by the client within seconds in the last 10m (about %d a minute) from %s (%s).",
			connects, (connects+5)/10, strings.Join(hosts, ", "), strings.Join(servers, ", ")))
	f.Value = &v1.FindingValue{Name: "short-lived connections", Display: fmt.Sprint(connects), Number: ptr(float64(connects)),
		Basis: "the connects folded into client.churn events of the connection name within the lookback, " +
			"each closed by the client within events.churn.pair_within"}
	return f
}
