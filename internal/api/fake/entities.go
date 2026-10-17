package fake

import (
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// Cluster is example-prod's cluster name, which is also its system ID.
const Cluster = "example-prod"

// Account is the JetStream account of every stream in the scenario.
const Account = "APP"

const (
	kib = 1 << 10
	mib = 1 << 20
	gib = 1 << 30
)

func ts(t time.Time) *timestamppb.Timestamp { return timestamppb.New(t) }

func dur(d time.Duration) *durationpb.Duration { return durationpb.New(d) }

func ptr[T any](v T) *T { return &v }

func streamRef(stream string) *v1.EntityRef {
	return &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_STREAM, Key: Account + "/" + stream, Name: stream, Account: Account, Stream: stream}
}

func consumerRef(stream, consumer string) *v1.EntityRef {
	return &v1.EntityRef{
		Type: v1.EntityType_ENTITY_TYPE_CONSUMER, Key: Account + "/" + stream + "/" + consumer, Name: consumer,
		Account: Account, Stream: stream, Consumer: consumer,
	}
}

func serverRef(name string) *v1.EntityRef {
	return &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SERVER, Key: name, Name: name, Server: name}
}

func clientRef(name string) *v1.EntityRef {
	return &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_CLIENT, Key: name, Name: name}
}

// matchesEntity reports whether e is want, or belongs to it: a stream
// covers its consumers.
func matchesEntity(e, want *v1.EntityRef) bool {
	if want == nil || want.GetType() == v1.EntityType_ENTITY_TYPE_UNSPECIFIED || want.GetType() == v1.EntityType_ENTITY_TYPE_SYSTEM {
		return true
	}
	if e.GetType() == want.GetType() && e.GetKey() == want.GetKey() {
		return true
	}
	return want.GetType() == v1.EntityType_ENTITY_TYPE_STREAM && e.GetType() == v1.EntityType_ENTITY_TYPE_CONSUMER &&
		strings.HasPrefix(e.GetKey(), want.GetKey()+"/")
}

// severityRank orders severities worst first.
func severityRank(s v1.Severity) int {
	switch s {
	case v1.Severity_SEVERITY_CRITICAL:
		return 0
	case v1.Severity_SEVERITY_WARNING:
		return 1
	case v1.Severity_SEVERITY_INFO:
		return 2
	}
	return 3
}

// statusRank orders statuses worst first; muted sorts after info.
func statusRank(s v1.Status) int {
	switch s {
	case v1.Status_STATUS_CRITICAL:
		return 0
	case v1.Status_STATUS_WARNING:
		return 1
	case v1.Status_STATUS_INFO:
		return 2
	case v1.Status_STATUS_MUTED:
		return 3
	case v1.Status_STATUS_OK:
		return 4
	}
	return 5
}

func statusOf(s v1.Severity) v1.Status {
	switch s {
	case v1.Severity_SEVERITY_CRITICAL:
		return v1.Status_STATUS_CRITICAL
	case v1.Severity_SEVERITY_WARNING:
		return v1.Status_STATUS_WARNING
	case v1.Severity_SEVERITY_INFO:
		return v1.Status_STATUS_INFO
	}
	return v1.Status_STATUS_OK
}

// worstStatus is the status the findings for an entity give it, with the
// codes worst first; a muted finding's code ends in " muted".
func worstStatus(fs []*v1.Finding, e *v1.EntityRef) (v1.Status, []string) {
	st := v1.Status_STATUS_OK
	var codes []string
	for _, f := range fs {
		if !matchesEntity(f.GetEntity(), e) {
			continue
		}
		fst := statusOf(f.GetSeverity())
		code := f.GetCheckCode()
		if f.GetMute() != nil {
			fst, code = v1.Status_STATUS_MUTED, code+" muted"
		}
		if statusRank(fst) < statusRank(st) {
			st = fst
		}
		codes = append(codes, code)
	}
	return st, codes
}
