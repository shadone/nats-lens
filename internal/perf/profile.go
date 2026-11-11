// Package perf builds synthetic stores of a chosen size and drives the API
// against them, for the measurements in
// docs/operations/performance-baseline.md. Nothing in it runs in the
// normal test tiers; cmd/natslens-perf and `make perf` call it.
package perf

import (
	"fmt"
	"time"
)

// Profile is the size of a synthetic system. Every entity name and counter
// derives from the profile and the epoch, so the same profile always
// builds the same store.
type Profile struct {
	Name     string
	Servers  int
	Accounts int
	Streams  int
	// Consumers in total, spread evenly over the streams.
	Consumers int
	// Connections in total, spread evenly over the servers; each server
	// reports at most ConnzLimit of its own, as CONNZ does.
	Connections int
	ConnzLimit  int
	Replicas    int
	// History is the span of stored epochs, Interval apart.
	History  time.Duration
	Interval time.Duration
	// EventsPerHour are the stored events of each hour of history.
	EventsPerHour int
}

// Profiles are the sizes the baseline uses. "entities" has the asset and
// connection counts of a large system over one day, the UI's default
// range; "history" a tenth of the assets over a full week. A store of
// "entities" grows by about 1.6 MB an epoch, 31 GB a week, so the two
// stand in for a week of the large system on one workstation's disk.
var Profiles = map[string]Profile{
	"smoke": {Name: "smoke", Servers: 3, Accounts: 3, Streams: 20, Consumers: 60, Connections: 300, ConnzLimit: 4096,
		Replicas: 3, History: 2 * time.Hour, Interval: 30 * time.Second, EventsPerHour: 200},
	"entities": {Name: "entities", Servers: 5, Accounts: 20, Streams: 2000, Consumers: 10000, Connections: 50000, ConnzLimit: 4096,
		Replicas: 3, History: 24 * time.Hour, Interval: 30 * time.Second, EventsPerHour: 2000},
	"history": {Name: "history", Servers: 5, Accounts: 10, Streams: 200, Consumers: 1000, Connections: 5000, ConnzLimit: 4096,
		Replicas: 3, History: 7 * 24 * time.Hour, Interval: 30 * time.Second, EventsPerHour: 600},
}

// Epochs is the number of epochs the history holds.
func (p Profile) Epochs() int { return int(p.History / p.Interval) }

// LastEpochAt is when the newest generated epoch started; the history ends
// here whatever day the generator runs, so that runs compare.
var LastEpochAt = time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)

// FirstEpochAt is when the oldest generated epoch started.
func (p Profile) FirstEpochAt() time.Time {
	return LastEpochAt.Add(-time.Duration(p.Epochs()-1) * p.Interval)
}

func (p Profile) String() string {
	return fmt.Sprintf("%s: %d servers, %d accounts, %d streams R%d, %d consumers, %d connections (at most %d per server), %s of %s epochs, %d events/hour",
		p.Name, p.Servers, p.Accounts, p.Streams, p.Replicas, p.Consumers, p.Connections, p.ConnzLimit, p.History, p.Interval, p.EventsPerHour)
}
