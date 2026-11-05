package notify

import (
	"context"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/store"
)

func ack(t *testing.T, e *env, after, quiet time.Duration) store.Ack {
	t.Helper()
	a := store.Ack{FindingID: jobs, AckedBy: "operator", Note: "Checking quorum", CreatedAt: e.at.Add(after)}
	if quiet != 0 {
		a.QuietUntil = a.CreatedAt.Add(quiet)
	}
	a, err := e.st.Acknowledge(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAcknowledgementQuiet(t *testing.T) {
	e := newEnv(t, channel("ops", func(c *config.Channel) { c.RepeatEvery = config.Duration(time.Hour) }))
	x := f(quorum, "APP/order-jobs", "warning")
	e.step(0, x)
	ack(t, e, time.Second, 2*time.Hour)
	expect(t, "ack", e.step(time.Minute, x)["ops"], "acknowledged "+jobs)
	expect(t, "unchanged ack", e.step(time.Minute, x)["ops"])
	expect(t, "quiet reminder", e.step(time.Hour, x)["ops"])
	x.sev = "critical"
	expect(t, "rise", e.step(time.Minute, x)["ops"], "changed "+jobs)
	x.sev = "info"
	expect(t, "fall below filter stays quiet", e.step(time.Minute, x)["ops"])
	x.sev = "warning"
	expect(t, "quiet ended", e.step(time.Hour, x)["ops"], "reminder "+jobs+" (acknowledgement quiet ended)")
	expect(t, "end once", e.step(time.Minute, x)["ops"])
}

func TestAcknowledgementReleaseAndReplace(t *testing.T) {
	e := newEnv(t, channel("ops"))
	x := f(quorum, "APP/order-jobs", "warning")
	e.step(0, x)
	ack(t, e, time.Second, 0)
	expect(t, "until resolved", e.step(time.Minute, x)["ops"], "acknowledged "+jobs)
	a := ack(t, e, time.Second, 0)
	expect(t, "replacement", e.step(time.Minute, x)["ops"], "acknowledged "+jobs)
	if _, err := e.st.EndAck(context.Background(), a.ID, "operator", e.at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	expect(t, "release", e.step(time.Minute, x)["ops"], "reminder "+jobs+" (acknowledgement quiet ended)")
	expect(t, "release once", e.step(time.Minute, x)["ops"])
}

func TestAcknowledgementGap(t *testing.T) {
	e := newEnv(t, channel("ops"))
	x := f(quorum, "APP/order-jobs", "warning")
	e.step(0, x)
	ack(t, e, time.Second, time.Minute)
	ack(t, e, 2*time.Second, 2*time.Minute)
	expect(t, "gap includes both notes and end", e.step(time.Hour, x)["ops"], "acknowledged "+jobs, "acknowledged "+jobs, "reminder "+jobs+" (acknowledgement quiet ended)")
	// Reconstruct the reconciler like a restart; no acknowledgement cursor lives in memory.
	e.rec = &Reconciler{Store: e.st, SystemID: "example-prod", Channels: []config.Channel{channel("ops")}, Checks: testChecks(t)}
	expect(t, "restart", e.step(time.Minute, x)["ops"])
}

func TestAcknowledgementOutsideView(t *testing.T) {
	for _, mode := range []string{"severity", "filter", "mute"} {
		t.Run(mode, func(t *testing.T) {
			c := channel("ops")
			x := f(quorum, "APP/order-jobs", "warning")
			if mode == "severity" {
				x.sev = "info"
			}
			if mode == "filter" {
				c.ExcludeChecks = []string{quorum}
			}
			e := newEnv(t, c)
			e.step(0, x)
			if mode == "mute" {
				_, err := e.st.DB().Exec(`INSERT INTO mutes (id,check_code,entity_type,entity_key,reason,muted_by,created_at,expires_at,accepted) VALUES (100,?,'stream','APP/order-jobs','maintenance','operator',?,?,false)`, quorum, e.at, e.at.Add(time.Hour))
				if err != nil {
					t.Fatal(err)
				}
				e.step(time.Minute, x)
			}
			ack(t, e, time.Second, 0)
			expect(t, "out of view", e.step(time.Minute, x)["ops"])
		})
	}
}

func TestAcknowledgementResolveAndEpisode(t *testing.T) {
	e := newEnv(t, channel("ops"))
	x := f(quorum, "APP/order-jobs", "critical")
	e.step(0, x)
	ack(t, e, time.Second, 0)
	e.step(time.Minute, x)
	expect(t, "resolve always", e.step(time.Minute)["ops"], "resolved "+jobs)
	expect(t, "new episode", e.step(time.Minute, x)["ops"], "firing "+jobs)
	// A second episode entirely between reconciles must not inherit quiet.
	ack(t, e, time.Second, 0)
	e.apply(time.Minute)
	e.apply(time.Minute, x)
	expect(t, "gap new episode", e.reconcile()["ops"], "resolved "+jobs, "firing "+jobs)
}

func TestAcknowledgementBaselineAndFlapping(t *testing.T) {
	e := newEnv(t, channel("ops"))
	x := f(quorum, "APP/order-jobs", "warning")
	e.apply(0, x)
	ack(t, e, 0, 0)
	e.reconcile()
	expect(t, "baseline ack not replayed", e.step(time.Minute, x)["ops"])
	if _, err := e.st.DB().Exec(`UPDATE findings SET flapping = true WHERE id = ?`, jobs); err != nil {
		t.Fatal(err)
	}
	ack(t, e, time.Second, 0)
	e.apply(time.Minute, x)
	if _, err := e.st.DB().Exec(`UPDATE findings SET flapping = true WHERE id = ?`, jobs); err != nil {
		t.Fatal(err)
	}
	expect(t, "ack during flap", e.reconcile()["ops"], "acknowledged "+jobs, "flapping "+jobs)
	e.apply(time.Minute)
	if _, err := e.st.DB().Exec(`UPDATE findings SET flapping = true WHERE id = ?`, jobs); err != nil {
		t.Fatal(err)
	}
	expect(t, "acknowledged resolve during flap", e.reconcile()["ops"], "resolved "+jobs)
}
