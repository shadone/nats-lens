package api

import (
	"errors"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

func TestNormalizeAck(t *testing.T) {
	good := func() *v1.AcknowledgeFindingRequest {
		return &v1.AcknowledgeFindingRequest{FindingId: "consumer-010:SHOP/order-archive/analytics-aggregator",
			Note: "  on it  ", QuietFor: durationpb.New(2*time.Hour + 300*time.Millisecond), AckedBy: " alice "}
	}
	m := good()
	if err := normalizeAck(m, ""); err != nil {
		t.Fatal(err)
	}
	if m.GetNote() != "on it" || m.GetAckedBy() != "alice" || m.GetQuietFor().AsDuration() != 2*time.Hour {
		t.Errorf("normalized = %v", m)
	}
	m = good()
	if err := normalizeAck(m, "iap@example.com"); err != nil || m.GetAckedBy() != "iap@example.com" {
		t.Errorf("with IAP: %v, acked_by %q", err, m.GetAckedBy())
	}
	m = good()
	m.Note, m.QuietFor, m.UntilResolved = strings.Repeat("é", 500), nil, true
	if err := normalizeAck(m, ""); err != nil {
		t.Errorf("a note of 500 characters until resolved: %v", err)
	}
	for _, d := range []time.Duration{30 * time.Minute, 24 * time.Hour} {
		m = good()
		m.QuietFor = durationpb.New(d)
		if err := normalizeAck(m, ""); err != nil {
			t.Errorf("quiet %s: %v", d, err)
		}
	}
	bad := map[string]func(*v1.AcknowledgeFindingRequest){
		"no finding":                     func(m *v1.AcknowledgeFindingRequest) { m.FindingId = "" },
		"note of 501 characters":         func(m *v1.AcknowledgeFindingRequest) { m.Note = strings.Repeat("é", 501) },
		"quiet of 29 minutes":            func(m *v1.AcknowledgeFindingRequest) { m.QuietFor = durationpb.New(29 * time.Minute) },
		"quiet of 25 hours":              func(m *v1.AcknowledgeFindingRequest) { m.QuietFor = durationpb.New(25 * time.Hour) },
		"until resolved with quiet_for":  func(m *v1.AcknowledgeFindingRequest) { m.UntilResolved = true },
		"neither quiet_for nor resolved": func(m *v1.AcknowledgeFindingRequest) { m.QuietFor = nil },
		"invalid duration":               func(m *v1.AcknowledgeFindingRequest) { m.QuietFor = &durationpb.Duration{Seconds: 3600, Nanos: -1} },
		"no name":                        func(m *v1.AcknowledgeFindingRequest) { m.AckedBy = " " },
		"name of 257 characters":         func(m *v1.AcknowledgeFindingRequest) { m.AckedBy = strings.Repeat("a", 257) },
	}
	for name, mod := range bad {
		m := good()
		mod(m)
		if err := normalizeAck(m, ""); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v, want invalid", name, err)
		}
	}
}

func TestNormalizeEndAck(t *testing.T) {
	if err := normalizeEndAck(&v1.EndAcknowledgementRequest{EndedBy: "a"}, ""); !errors.Is(err, ErrInvalid) {
		t.Errorf("no id: %v", err)
	}
	if err := normalizeEndAck(&v1.EndAcknowledgementRequest{Id: "1"}, ""); !errors.Is(err, ErrInvalid) {
		t.Errorf("no name: %v", err)
	}
	m := &v1.EndAcknowledgementRequest{Id: "1", EndedBy: "body"}
	if err := normalizeEndAck(m, "iap@example.com"); err != nil || m.GetEndedBy() != "iap@example.com" {
		t.Errorf("with IAP: %v, ended_by %q", err, m.GetEndedBy())
	}
}
