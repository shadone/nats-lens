package notify

import (
	"context"
	"testing"
	"time"
)

func TestSecondReviewReleasedAckOtherChannel(t *testing.T) {
	e := newEnv(t, channel("ops"), channel("idle"))
	x := f(quorum, "APP/order-jobs", "warning")
	e.step(0, x)
	e.rec.Channels = e.rec.Channels[:1]
	a := ack(t, e, time.Second, 0)
	expect(t, "ops note", e.step(time.Minute, x)["ops"], "acknowledged "+jobs)
	if _, err := e.st.EndAck(context.Background(), a.ID, "operator", e.at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	expect(t, "ops release", e.step(time.Minute, x)["ops"], "reminder "+jobs+" (acknowledgement quiet ended)")
	e.rec.Channels = append(e.rec.Channels, channel("new"))
	expect(t, "new channel baseline after release", e.step(time.Minute, x)["new"], "baseline")
	in, err := e.st.NotifyInput(context.Background(), e.epoch)
	if err != nil {
		t.Fatal(err)
	}
	if in.States["new"][jobs].Acknowledged {
		t.Error("new channel inherited acknowledgement released before its baseline")
	}
	e.apply(time.Minute, x)
	if _, err := e.st.DB().Exec(`UPDATE findings SET flapping=true WHERE id=?`, jobs); err != nil {
		t.Fatal(err)
	}
	expect(t, "normal new-channel flap", e.reconcile()["new"], "flapping "+jobs)
	e.apply(time.Minute)
	if _, err := e.st.DB().Exec(`UPDATE findings SET flapping=true WHERE id=?`, jobs); err != nil {
		t.Fatal(err)
	}
	expect(t, "unacknowledged channel resolve held", e.reconcile()["new"])
}

// Each baseline sees the same shared history, but only its own observation can
// remember a note. Creation is inclusive and release is exclusive; quiet expiry
// does not end the acknowledgement itself.
func TestAcknowledgementChannelBaselineBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		created, ended, quiet time.Duration
		want                  bool
	}{
		{"created-at-baseline", time.Minute, 0, 0, true},
		{"created-after-baseline", time.Minute + time.Second, 0, 0, false},
		{"released-before-baseline", time.Second, 59 * time.Second, 0, false},
		{"released-at-baseline", time.Second, time.Minute, 0, false},
		{"released-after-baseline", time.Second, time.Minute + time.Second, 0, true},
		{"quiet-expired-before-baseline", time.Second, 0, time.Second, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, channel("ops"), channel("idle"))
			x := f(quorum, "APP/order-jobs", "warning")
			e.step(0, x)
			a := ack(t, e, tc.created, tc.quiet)
			if tc.ended != 0 {
				if _, err := e.st.EndAck(context.Background(), a.ID, "operator", e.at.Add(tc.ended)); err != nil {
					t.Fatal(err)
				}
			}
			e.rec.Channels = append(e.rec.Channels[:1], channel("new"))
			expect(t, "baseline", e.step(time.Minute, x)["new"], "baseline")
			in, err := e.st.NotifyInput(context.Background(), e.epoch)
			if err != nil {
				t.Fatal(err)
			}
			if got := in.States["new"][jobs].Acknowledged; got != tc.want {
				t.Fatalf("baseline memory = %v, want %v", got, tc.want)
			}
			if got := e.reconcile(); len(got["new"]) != 0 {
				t.Fatal("baseline duplicated", got)
			}
		})
	}
}

func TestAcknowledgementFilterReentryDoesNotReplayEndedMemory(t *testing.T) {
	e := newEnv(t, channel("ops"), channel("idle"))
	x := f(quorum, "APP/order-jobs", "warning")
	e.step(0, x)
	e.rec.Channels = e.rec.Channels[:1]
	a := ack(t, e, time.Second, 0)
	e.step(time.Minute, x)
	e.rec.Channels[0].ExcludeChecks = []string{quorum}
	expect(t, "filter exit", e.step(time.Minute, x)["ops"], "resolved "+jobs+" (no longer selected by the channel)")
	if _, err := e.st.EndAck(context.Background(), a.ID, "operator", e.at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	e.step(time.Minute, x) // Advance past release while out of view.
	e.rec.Channels[0].ExcludeChecks = nil
	expect(t, "reentry", e.step(time.Minute, x)["ops"], "firing "+jobs)
	in, err := e.st.NotifyInput(context.Background(), e.epoch)
	if err != nil {
		t.Fatal(err)
	}
	if in.States["ops"][jobs].Acknowledged {
		t.Fatal("filter reentry inherited released memory from shared history")
	}
	e.apply(time.Minute, x)
	if _, err := e.st.DB().Exec(`UPDATE findings SET flapping=true WHERE id=?`, jobs); err != nil {
		t.Fatal(err)
	}
	expect(t, "ordinary flap", e.reconcile()["ops"], "flapping "+jobs)
	e.apply(time.Minute)
	if _, err := e.st.DB().Exec(`UPDATE findings SET flapping=true WHERE id=?`, jobs); err != nil {
		t.Fatal(err)
	}
	expect(t, "ordinary hold", e.reconcile()["ops"])
}
