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
