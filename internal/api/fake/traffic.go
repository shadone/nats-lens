package fake

import (
	"hash/fnv"
	"math"
	"strings"
	"time"
)

// The demo's client traffic: each connection sends and receives at its
// own rate times a level that follows the day, highest at 14:00 and
// lowest at 02:00 in the fixed frame. A connection's counters are what it
// moved since it connected; the hourly rollup sums the same rates over
// the time its server was up, so that the two agree.

// trafficSwing is how far the level moves above and below 1.
const trafficSwing = 0.35

// trafficPeak is the fixed-frame time of day at which the level peaks.
const trafficPeak = 14 * time.Hour

// trafficIntegral is the level's integral from the fixed frame's Unix
// epoch to fixed-frame time c, in seconds at level 1.
func trafficIntegral(c time.Time) float64 {
	const w = 2 * math.Pi / (24 * 3600)
	t := float64(c.UnixNano()) / 1e9
	phase := w * (t - (trafficPeak - 6*time.Hour).Seconds())
	return t - trafficSwing/w*math.Cos(phase)
}

// trafficBetween is the level's integral from a to b, scenario times,
// zero when b is not after a.
func (s scen) trafficBetween(a, b time.Time) float64 {
	if !b.After(a) {
		return 0
	}
	return trafficIntegral(s.clock(b)) - trafficIntegral(s.clock(a))
}

// connRate is the messages per second at level 1 one connection of a
// client sends (in) and receives (out): its own per client, a little
// more for every third connection.
func connRate(name string, i int64) (in, out float64) {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	x := h.Sum32()
	in = (0.4 + float64(x%37)/10) * (1 + float64(i%3)/4)
	return in, in * (1.5 + float64(x/37%5)/4)
}

// The bytes per message of the clients' and the extra connections'
// traffic.
const (
	clientMsgBytes   = 900
	extraInMsgBytes  = 700
	extraOutMsgBytes = 300
)

// extraRate is the rate of every extra connection.
const extraIn, extraOut = 1.2, 0.7

// upBetween is the level's integral over the times from a to b at which
// the server was up, as the epochs see it: by steps of sampleEvery,
// each step counted when the server was not down at its start.
func (s scen) upBetween(sp serverSpec, a, b time.Time) float64 {
	if !b.After(a) {
		return 0
	}
	if s.sc.serverDown == nil {
		return s.trafficBetween(a, b)
	}
	var sum float64
	for t := a; t.Before(b); t = t.Add(sampleEvery) {
		end := t.Add(sampleEvery)
		if end.After(b) {
			end = b
		}
		if !s.downAt(sp, t) {
			sum += s.trafficBetween(t, end)
		}
	}
	return sum
}

// The client library versions of the demo's connections.
const (
	goVersion = "1.54.0"
	jsVersion = "3.0.2"
)

// libraryOf is the client library a client's connections on a server
// report: the scenario's own rule, else go for the media workers and the
// label printer and nats.js for the others.
func (sc *scenario) libraryOf(sp serverSpec, client string) (lang, version string) {
	if sc.library != nil {
		if lang, version := sc.library(sp, client); lang != "" {
			return lang, version
		}
	}
	switch client {
	case "media-worker", "video-transcoder", "label-printer":
		return "go", goVersion
	}
	return "nats.js", jsVersion
}

// toolingClient reports whether a client is tooling by the demo's rule:
// nats-lens's own connections, or a client from a loopback address.
func toolingClient(name, host string) bool {
	return strings.HasPrefix(name, "natslens-") || host == "127.0.0.1" || host == "::1"
}
