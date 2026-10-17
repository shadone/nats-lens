package api

import (
	"regexp"
	"strings"
	"time"
)

// RoutineKinds are the event kinds that are routine whoever caused them:
// client churn, sampled acks and account connection counts.
var RoutineKinds = []string{"client.churn", "consumer.ack_sampled", "account.connections"}

// AuditKind is the kind of a JetStream API audit record. A record is
// routine when the call succeeded (no reason) and its subject matches
// no MutatingAuditPattern.
const AuditKind = "api.audit"

// MutatingAuditPattern matches the JetStream API subjects of calls that
// change something: $JS.API.<verb>... as nats-server names them, for
// example $JS.API.STREAM.DELETE.<stream> or
// $JS.API.CONSUMER.DURABLE.CREATE.<stream>.<consumer>. RE2 syntax, for
// Go and DuckDB alike.
const MutatingAuditPattern = `(^|\.)API\.(STREAM\.(CREATE|UPDATE|DELETE|PURGE|MSG\.DELETE|SNAPSHOT|RESTORE|PEER\.REMOVE|LEADER\.STEPDOWN)` +
	`|CONSUMER\.(CREATE|DURABLE\.CREATE|DELETE|PAUSE|UNPIN|LEADER\.STEPDOWN)|META\.LEADER\.STEPDOWN|SERVER\.REMOVE|ACCOUNT\.PURGE)(\.|$)`

var mutatingAudit = regexp.MustCompile(MutatingAuditPattern)

// RoutineAudit reports whether an API audit record with this subject and
// reason is routine: a successful call that changes nothing.
func RoutineAudit(subject, reason string) bool {
	return reason == "" && !mutatingAudit.MatchString(subject)
}

// ToolingKinds are the event kinds that are routine when a tooling client
// caused them, judged as the client checks judge a connection.
var ToolingKinds = []string{"client.connect", "client.disconnect"}

const (
	// FoldGap is the longest gap between two events of one folded row.
	FoldGap = 10 * time.Minute
	// MaxHistogramBuckets is the most buckets a histogram may have.
	MaxHistogramBuckets = 120
	// MaxRoutineGroups is the most routine groups a response lists.
	MaxRoutineGroups = 10
)

// routineNouns name a routine kind's events in a group label.
var routineNouns = map[string]string{
	"client.churn":         "short connections",
	"consumer.ack_sampled": "sampled acks",
	"api.audit":            "API calls",
	"account.connections":  "account connection counts",
	"client.connect":       "connects",
	"client.disconnect":    "disconnects",
}

// RoutineLabel names a group of routine events of one kind and client,
// for example "catalog-api short connections".
func RoutineLabel(kind, client string) string {
	noun, ok := routineNouns[kind]
	if !ok {
		noun = kind
	}
	if client == "" {
		if strings.HasPrefix(kind, "client.") {
			return "unnamed client " + noun
		}
		// Sentence case for a label without a client in front.
		return strings.ToUpper(noun[:1]) + noun[1:]
	}
	return client + " " + noun
}

// HistogramWidth is the width of each of n buckets over span: span
// divided by n, rounded up to a whole second.
func HistogramWidth(span time.Duration, n int) time.Duration {
	w := time.Duration(ceilDiv(span, time.Duration(n)))
	if r := w % time.Second; r != 0 {
		w += time.Second - r
	}
	return max(w, time.Second)
}

func histogramBuckets(n int32) error {
	if n < 0 || n > MaxHistogramBuckets {
		return invalid("histogram_buckets must be from 0 to %d", MaxHistogramBuckets)
	}
	return nil
}
