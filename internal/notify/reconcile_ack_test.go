package notify

import (
	"context"
	"path/filepath"
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

func TestAcknowledgementNewEpisodeInGap(t *testing.T) {
	e := newEnv(t, channel("ops"))
	x := f(quorum, "APP/order-jobs", "warning")
	e.step(0, x)
	e.apply(time.Minute)
	e.apply(time.Minute, x)
	ack(t, e, time.Second, 0)
	e.apply(time.Minute, x)
	got := e.reconcile()["ops"]
	if len(got) != 3 {
		t.Fatalf("new acknowledged episode lost a message: %v", kinds(got))
	}
	counts := map[Kind]int{}
	for _, m := range got {
		counts[m.Kind]++
	}
	if counts[KindAcknowledged] != 1 || counts[KindResolved] != 1 || counts[KindFiring] != 1 {
		t.Fatal(kinds(got))
	}
}

func TestAcknowledgementRisingWhileFlapping(t *testing.T) {
	e := newEnv(t, channel("ops"))
	x := f(quorum, "APP/order-jobs", "warning")
	e.step(0, x)
	ack(t, e, time.Second, 0)
	e.step(time.Minute, x)
	e.apply(time.Minute, x)
	if _, err := e.st.DB().Exec(`UPDATE findings SET flapping=true WHERE id=?`, jobs); err != nil {
		t.Fatal(err)
	}
	e.reconcile()
	x.sev = "critical"
	e.apply(time.Minute, x)
	if _, err := e.st.DB().Exec(`UPDATE findings SET flapping=true WHERE id=?`, jobs); err != nil {
		t.Fatal(err)
	}
	expect(t, "quiet rise overrides flapping hold", e.reconcile()["ops"], "changed "+jobs)
}

func TestAcknowledgementDiskRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notify.duckdb")
	st, err := store.OpenPath(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	e := envOn(t, st, "example-prod", channel("ops"))
	x := f(quorum, "APP/order-jobs", "warning")
	e.step(0, x)
	a := ack(t, e, time.Second, time.Hour)
	got := e.step(time.Minute, x)["ops"]
	if len(got) != 1 || got[0].Acknowledgement == nil || got[0].Acknowledgement.By != a.AckedBy || got[0].Acknowledgement.Note != a.Note || !got[0].Acknowledgement.QuietUntil.Equal(a.QuietUntil) {
		t.Fatalf("payload: %+v", got)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = store.OpenPath(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	e.st, e.rec.Store = st, st
	expect(t, "restart does not duplicate", e.step(time.Minute, x)["ops"])
	expect(t, "restart keeps quiet end", e.step(time.Hour, x)["ops"], "reminder "+jobs+" (acknowledgement quiet ended)")
	expect(t, "reconcile same epoch", e.reconcile()["ops"])
}

func TestAcknowledgementKindExcluded(t *testing.T) {
	c := channel("ops")
	c.Kinds = []string{"firing", "reminder", "resolved", "changed"}
	e := newEnv(t, c)
	x := f(quorum, "APP/order-jobs", "critical")
	e.step(0, x)
	ack(t, e, time.Second, time.Hour)
	expect(t, "kind excluded", e.step(time.Minute, x)["ops"])
	x.sev = "warning"
	expect(t, "quiet still applies", e.step(time.Minute, x)["ops"])
	expect(t, "end still applies", e.step(time.Hour, x)["ops"], "reminder "+jobs+" (acknowledgement quiet ended)")
}

func TestAcknowledgementViewEntryAndThresholdEnd(t *testing.T) {
	e := newEnv(t, channel("ops"))
	x := f(quorum, "APP/order-jobs", "info")
	e.step(0, x)
	ack(t, e, time.Second, time.Hour)
	expect(t, "out-of-view ack", e.step(time.Minute, x)["ops"])
	x.sev = "critical"
	expect(t, "entry does not replay old note", e.step(time.Minute, x)["ops"], "firing "+jobs)
	x.sev = "info"
	expect(t, "quiet retains known incident", e.step(time.Minute, x)["ops"])
	expect(t, "quiet end closes below threshold", e.step(time.Hour, x)["ops"], "reminder "+jobs+" (acknowledgement quiet ended)", "resolved "+jobs+" (severity now info)")
	expect(t, "threshold exit once", e.step(time.Minute, x)["ops"])
}

func TestAcknowledgementEpochCutoffAndChannelCursor(t *testing.T) {
	e := newEnv(t, channel("ops"))
	x := f(quorum, "APP/order-jobs", "warning")
	e.step(0, x)
	a := ack(t, e, 2*time.Minute, time.Hour)
	expect(t, "future ack not read", e.step(time.Minute, x)["ops"])
	// Another channel starts at this epoch; neither channel should miss a later note.
	e.rec.Channels = append(e.rec.Channels, channel("chat"))
	got := e.step(time.Minute, x)
	expect(t, "created at epoch inclusive", got["ops"], "acknowledged "+jobs)
	expect(t, "new channel only baseline", got["chat"], "baseline")
	next := ack(t, e, time.Second, 0)
	got = e.step(time.Minute, x)
	for _, name := range []string{"ops", "chat"} {
		expect(t, name, got[name], "acknowledged "+jobs)
	}
	if got["ops"][0].DedupKey == DedupKey("example-prod", jobs, a.SinceEpoch, KindAcknowledged, e.epoch) || a.ID == next.ID {
		t.Fatal("acknowledgement ID missing from dedup key")
	}
	expect(t, "unchanged on both channels", e.step(time.Minute, x)["ops"])
}

func TestAcknowledgementQuietEndWithRise(t *testing.T) {
	e := newEnv(t, channel("ops"))
	x := f(quorum, "APP/order-jobs", "warning")
	e.step(0, x)
	ack(t, e, time.Second, time.Hour)
	e.step(time.Minute, x)
	x.sev = "critical"
	expect(t, "rise and expiry both sent", e.step(time.Hour, x)["ops"], "changed "+jobs, "reminder "+jobs+" (acknowledgement quiet ended)")
}

func TestAcknowledgementReleasedEpisodeResolveWhileFlapping(t *testing.T) {
	e := newEnv(t, channel("ops"))
	x := f(quorum, "APP/order-jobs", "warning")
	e.step(0, x)
	a := ack(t, e, time.Second, 0)
	e.step(time.Minute, x)
	e.apply(time.Minute, x)
	if _, err := e.st.DB().Exec(`UPDATE findings SET flapping=true WHERE id=?`, jobs); err != nil {
		t.Fatal(err)
	}
	e.reconcile()
	if _, err := e.st.EndAck(context.Background(), a.ID, "operator", e.at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	e.apply(time.Minute, x)
	if _, err := e.st.DB().Exec(`UPDATE findings SET flapping=true WHERE id=?`, jobs); err != nil {
		t.Fatal(err)
	}
	expect(t, "release while flapping", e.reconcile()["ops"], "reminder "+jobs+" (acknowledgement quiet ended)")
	e.apply(time.Minute)
	if _, err := e.st.DB().Exec(`UPDATE findings SET flapping=true WHERE id=?`, jobs); err != nil {
		t.Fatal(err)
	}
	expect(t, "actual resolve of acknowledged episode", e.reconcile()["ops"], "resolved "+jobs)
}

// Released history falls behind NotifyInput's cursor; expired/replaced history
// can also disappear in retention. The channel must still close its known
// episode, even after reopening the database, without remembering other episodes.
func TestAcknowledgementEndedFlappingDiskRestart(t *testing.T) {
	for _, ending := range []string{"release", "expiry"} {
		t.Run(ending, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "notify.duckdb")
			st, err := store.OpenPath(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			e := envOn(t, st, "example-prod", channel("ops"))
			t.Cleanup(func() { _ = e.st.Close() })
			x := f(quorum, "APP/order-jobs", "critical")
			e.step(0, x)
			quiet := time.Duration(0)
			if ending == "expiry" {
				quiet = 3 * time.Minute
			}
			a := ack(t, e, time.Second, quiet)
			expect(t, "ack", e.step(time.Minute, x)["ops"], "acknowledged "+jobs)
			flap := func(fires ...fire) map[string][]Message {
				e.apply(time.Minute, fires...)
				if _, err := e.st.DB().Exec(`UPDATE findings SET flapping = true WHERE id = ?`, jobs); err != nil {
					t.Fatal(err)
				}
				return e.reconcile()
			}
			x.sev = "warning"
			expect(t, "flapping starts with quiet downward change", flap(x)["ops"], "flapping "+jobs)
			x.sev = "info"
			expect(t, "quiet downward change below threshold", flap(x)["ops"])
			x.sev = "warning"
			if ending == "release" {
				if _, err := e.st.EndAck(context.Background(), a.ID, "operator", e.at.Add(time.Second)); err != nil {
					t.Fatal(err)
				}
			}
			expect(t, "quiet end", flap(x)["ops"], "reminder "+jobs+" (acknowledgement quiet ended)")
			expect(t, "cursor advances past release", flap(x)["ops"])
			// Simulate history retention independently of finding retention.
			if _, err := e.st.DB().Exec(`DELETE FROM acknowledgements`); err != nil {
				t.Fatal(err)
			}
			if err := e.st.Close(); err != nil {
				t.Fatal(err)
			}
			e.st, err = store.OpenPath(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			e.rec = &Reconciler{Store: e.st, SystemID: "example-prod", Checks: testChecks(t), Channels: []config.Channel{channel("ops"), channel("new")}}
			got := flap()
			expect(t, "actual resolve after restart", got["ops"], "resolved "+jobs)
			expect(t, "new channel never knew resolved episode", got["new"], "baseline")
			got = flap()
			expect(t, "resolve once", got["ops"])
			expect(t, "new channel has no resolve", got["new"])
			// A different episode gets the ordinary flapping hold, for both
			// the old channel and a channel never told of the acknowledged one.
			got = flap(x)
			for _, name := range []string{"ops", "new"} {
				expect(t, "new episode "+name, got[name], "flapping "+jobs)
			}
			got = flap()
			for _, name := range []string{"ops", "new"} {
				expect(t, "unacknowledged resolve held "+name, got[name])
			}
			e.apply(time.Minute)
			if _, err := e.st.DB().Exec(`UPDATE findings SET flapping = false WHERE id = ?`, jobs); err != nil {
				t.Fatal(err)
			}
			got = e.reconcile()
			for _, name := range []string{"ops", "new"} {
				expect(t, "normal resolve "+name, got[name], "resolved "+jobs)
			}
		})
	}
}

func TestAcknowledgementMemoryRemovedWithChannel(t *testing.T) {
	e := newEnv(t, channel("ops"))
	x := f(quorum, "APP/order-jobs", "warning")
	e.step(0, x)
	a := ack(t, e, time.Second, 0)
	e.step(time.Minute, x)
	if _, err := e.st.EndAck(context.Background(), a.ID, "operator", e.at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	e.step(time.Minute, x)
	if _, err := e.st.SweepNotifications(context.Background(), e.at, time.Hour, nil); err != nil {
		t.Fatal(err)
	}
	// The acknowledgement is older than the new baseline and must not give
	// the re-added channel the removed channel's durable memory.
	e.seen = 0
	expect(t, "re-added baseline", e.step(time.Minute, x)["ops"], "baseline")
	e.apply(time.Minute, x)
	if _, err := e.st.DB().Exec(`UPDATE findings SET flapping = true WHERE id = ?`, jobs); err != nil {
		t.Fatal(err)
	}
	expect(t, "ordinary flap", e.reconcile()["ops"], "flapping "+jobs)
	e.apply(time.Minute)
	if _, err := e.st.DB().Exec(`UPDATE findings SET flapping = true WHERE id = ?`, jobs); err != nil {
		t.Fatal(err)
	}
	expect(t, "removed memory does not bypass hold", e.reconcile()["ops"])
}
