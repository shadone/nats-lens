package fake_test

import (
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// Every demo page: a slow endpoint has a start, subjects lead with subject,
// instance_source agrees with answered and instances; around 11:29:56 the
// timeout shows.
func TestDemoMicroSweep(t *testing.T) {
	c := demo(t)
	day := fake.FixedNow.Truncate(24 * time.Hour)
	var ends []time.Time
	for m := 0; m < 24*60; m += 10 {
		ends = append(ends, day.Add(time.Duration(m)*time.Minute))
	}
	for s := -90; s <= 120; s += 15 {
		ends = append(ends, day.Add(11*time.Hour+29*time.Minute+56*time.Second+time.Duration(s)*time.Second))
	}
	for _, sys := range []string{fake.Cluster, fake.GlobalSystem, fake.StagingSystem} {
		for _, end := range ends {
			if end.After(fake.FixedNow) {
				continue
			}
			res := microOf(t, c, sys, end)
			timedOut := false
			for _, a := range res.GetAccounts() {
				if a.GetStats().GetState() == v1.MicroRequestState_MICRO_REQUEST_STATE_TIMEOUT {
					timedOut = true
				}
			}
			for _, s := range res.GetServices() {
				src := s.GetInstanceSource()
				switch {
				case s.GetAnswered() && src != v1.MicroInstanceSource_MICRO_INSTANCE_SOURCE_ANSWERED,
					!s.GetAnswered() && s.GetInstances() > 0 && src == v1.MicroInstanceSource_MICRO_INSTANCE_SOURCE_ANSWERED,
					s.GetInstances() == 0 && src != v1.MicroInstanceSource_MICRO_INSTANCE_SOURCE_UNSPECIFIED,
					s.GetInstances() > 0 && src == v1.MicroInstanceSource_MICRO_INSTANCE_SOURCE_UNSPECIFIED:
					t.Errorf("%s %s %s: answered %v instances %d source %v", sys, end.Format("15:04:05"), s.GetName(), s.GetAnswered(), s.GetInstances(), src)
				}
				if timedOut {
					t.Logf("%s %s: %s timeout: answered %v instances %d missing %d note %v source %v", sys, end.Format("15:04:05"), s.GetName(),
						s.GetAnswered(), s.GetInstances(), len(s.GetMissing()), s.GetNote(), src)
				}
				for _, e := range s.GetEndpoints() {
					if e.GetSlow() && e.GetSlowSince() == nil {
						t.Errorf("%s %s %s/%s slow without a start", sys, end.Format("15:04:05"), s.GetName(), e.GetName())
					}
					if e.GetSlowSince() != nil && e.GetSlowSince().AsTime().After(end) {
						t.Errorf("%s %s %s/%s start after the end", sys, end.Format("15:04:05"), s.GetName(), e.GetName())
					}
					if len(e.GetSubjects()) == 0 || e.GetSubjects()[0] != e.GetSubject() {
						t.Errorf("%s %s/%s subjects %v subject %q", sys, s.GetName(), e.GetName(), e.GetSubjects(), e.GetSubject())
					}
				}
			}
		}
	}
}
