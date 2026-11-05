package notify

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/checks"
	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/store"
)

// Run these with a non-UTC TZ as well (AGENTS.md): the epochs' times are
// compared with the times the store writes and reads back.
var r0 = time.Date(2026, 11, 5, 9, 30, 0, 0, time.UTC)

// The checks the tests fire: two health checks of streams, a check of
// servers in another category, and an expiry check.
const (
	quorum   = "stream-001"
	version  = "server-005"
	consumer = "consumer-001"
)

var loadedChecks *checks.Set

func testChecks(t *testing.T) *checks.Set {
	t.Helper()
	if loadedChecks == nil {
		set, err := checks.Load(config.Checks{})
		if err != nil {
			t.Fatal(err)
		}
		loadedChecks = set
	}
	return loadedChecks
}

func channel(name string, edit ...func(*config.Channel)) config.Channel {
	c := config.DefaultChannel()
	c.Name, c.Type, c.URL = name, config.ChannelWebhook, "https://hooks.example.com/"+name
	for _, e := range edit {
		e(&c)
	}
	return c
}

// env is one system's store and reconciler, stepped one epoch at a time.
type env struct {
	t     *testing.T
	st    *store.Store
	rec   *Reconciler
	epoch int64
	at    time.Time
	// seen counts the outbox rows already returned.
	seen int
}

func newEnv(t *testing.T, chans ...config.Channel) *env {
	t.Helper()
	st, err := store.OpenPath(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return envOn(t, st, "example-prod", chans...)
}

func envOn(t *testing.T, st *store.Store, system string, chans ...config.Channel) *env {
	return &env{t: t, st: st, at: r0, rec: &Reconciler{Store: st, SystemID: system, SystemName: "Production", Channels: chans,
		Checks: testChecks(t), Retention: 14 * 24 * time.Hour, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}}
}

// fire is a failing entity of a check.
type fire struct {
	code, key, sev string
}

func f(code, key, sev string) fire { return fire{code, key, sev} }

// apply stores the next epoch, after, with the check runs of the fires;
// every check named in codes runs.
func (e *env) apply(after time.Duration, fires ...fire) {
	e.t.Helper()
	e.epoch++
	e.at = e.at.Add(after)
	if _, err := e.st.DB().Exec(`INSERT INTO epochs VALUES (?, ?, ?, 0, 1, 1, ['nats-0'], ['nats-0'], [], 0, false, [], NULL)`,
		e.epoch, e.at, e.at); err != nil {
		e.t.Fatal(err)
	}
	byCode := map[string][]store.FindingRow{}
	for _, x := range fires {
		byCode[x.code] = append(byCode[x.code], store.FindingRow{EntityKey: x.key, Severity: x.sev, Detail: x.key + " is " + x.sev})
	}
	var runs []store.CheckRun
	for _, code := range []string{quorum, version, consumer, "server-016", "stream-002"} {
		c, _ := testChecks(e.t).Lookup(code)
		runs = append(runs, store.CheckRun{Code: code, EntityType: c.Entity, Status: store.RunOK, Rows: byCode[code]})
	}
	for code := range byCode {
		if !slices.ContainsFunc(runs, func(r store.CheckRun) bool { return r.Code == code }) {
			c, _ := testChecks(e.t).Lookup(code)
			runs = append(runs, store.CheckRun{Code: code, EntityType: c.Entity, Status: store.RunOK, Rows: byCode[code]})
		}
	}
	if _, err := e.st.ApplyCheckRuns(context.Background(), e.epoch, runs); err != nil {
		e.t.Fatal(err)
	}
}

// reconcile queues the epoch's notifications and returns the new outbox
// rows by channel.
func (e *env) reconcile() map[string][]Message {
	e.t.Helper()
	if _, err := e.rec.Reconcile(context.Background(), e.epoch); err != nil {
		e.t.Fatal(err)
	}
	rows, err := e.st.OutboxRows(context.Background(), "")
	if err != nil {
		e.t.Fatal(err)
	}
	out := map[string][]Message{}
	for _, r := range rows[e.seen:] {
		m, err := Decode(r.Message)
		if err != nil {
			e.t.Fatal(err)
		}
		if !r.CreatedAt.Equal(e.at) || r.Epoch != e.epoch || r.Kind != string(m.Kind) || r.DedupKey != m.DedupKey ||
			r.FindingID != m.FindingID || r.State != store.OutboxPending {
			e.t.Fatalf("outbox row %+v for message %+v at epoch %d (%s)", r, m, e.epoch, e.at)
		}
		out[r.Channel] = append(out[r.Channel], m)
	}
	e.seen = len(rows)
	return out
}

// step applies an epoch and reconciles it.
func (e *env) step(after time.Duration, fires ...fire) map[string][]Message {
	e.t.Helper()
	e.apply(after, fires...)
	return e.reconcile()
}

// kinds words messages as "kind id [reason]", for comparison.
func kinds(ms []Message) []string {
	var out []string
	for _, m := range ms {
		s := string(m.Kind)
		if m.FindingID != "" {
			s += " " + m.FindingID
		}
		if m.Reason != "" {
			s += " (" + m.Reason + ")"
		}
		out = append(out, s)
	}
	return out
}

func expect(t *testing.T, what string, got []Message, want ...string) {
	t.Helper()
	if g := kinds(got); !slices.Equal(g, want) {
		t.Errorf("%s:\n got  %q\n want %q", what, g, want)
	}
}

const (
	jobs   = "stream-001:APP/order-jobs"
	images = "stream-001:APP/images"
)

func TestFiringChangedResolved(t *testing.T) {
	e := newEnv(t, channel("ops"))
	got := e.step(time.Minute)
	expect(t, "first epoch", got["ops"], "baseline")
	if b := got["ops"][0]; b.FiringCount == nil || *b.FiringCount != 0 || b.Detail != "nats-lens now notifies this channel: no findings firing" ||
		b.DedupKey != "example-prod:baseline:1" {
		t.Errorf("baseline %+v", b)
	}
	got = e.step(time.Minute, f(quorum, "APP/order-jobs", "warning"))
	expect(t, "fires", got["ops"], "firing "+jobs)
	m := got["ops"][0]
	if m.CheckName != "Stream quorum lost" || m.Category != "health" || m.Severity != "warning" || m.EntityType != "stream" ||
		m.EntityKey != "APP/order-jobs" || m.Detail != "APP/order-jobs is warning" || !m.Since.Equal(e.at) ||
		!m.TransitionedAt.Equal(e.at) || m.DedupKey != "example-prod:"+jobs+":2:firing:2" || m.Link != "" {
		t.Errorf("firing message %+v", m)
	}
	expect(t, "unchanged", e.step(time.Minute, f(quorum, "APP/order-jobs", "warning"))["ops"])
	got = e.step(time.Minute, f(quorum, "APP/order-jobs", "critical"))
	expect(t, "rises", got["ops"], "changed "+jobs)
	if m := got["ops"][0]; m.PreviousSeverity != "warning" || m.Severity != "critical" || m.DedupKey != "example-prod:"+jobs+":2:changed:4" {
		t.Errorf("changed up %+v", m)
	}
	got = e.step(time.Minute, f(quorum, "APP/order-jobs", "warning"))
	expect(t, "falls within the range", got["ops"], "changed "+jobs)
	if m := got["ops"][0]; m.PreviousSeverity != "critical" || m.Severity != "warning" {
		t.Errorf("changed down %+v", m)
	}
	got = e.step(time.Minute, f(quorum, "APP/order-jobs", "info"))
	expect(t, "falls below", got["ops"], "resolved "+jobs+" (severity now info)")
	if m := got["ops"][0]; m.PreviousSeverity != "warning" || m.Severity != "info" || !m.ResolvedAt.IsZero() {
		t.Errorf("below the range %+v", m)
	}
	expect(t, "stays below", e.step(time.Minute, f(quorum, "APP/order-jobs", "info"))["ops"])
	expect(t, "rises into the range", e.step(time.Minute, f(quorum, "APP/order-jobs", "critical"))["ops"], "firing "+jobs)
	got = e.step(time.Minute)
	expect(t, "resolves", got["ops"], "resolved "+jobs)
	if m := got["ops"][0]; !m.ResolvedAt.Equal(e.at) || m.Severity != "critical" || m.DedupKey != "example-prod:"+jobs+":2:resolved:9" {
		t.Errorf("resolved %+v (epoch at %s)", m, e.at)
	}
	expect(t, "stays resolved", e.step(time.Minute)["ops"])
	if n := stateRows(t, e.st); n != 0 {
		t.Errorf("%d notify_state rows after the resolve", n)
	}
}

func stateRows(t *testing.T, st *store.Store) int {
	t.Helper()
	var n int
	if err := st.DB().QueryRow(`SELECT count(*) FROM notify_state`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func mute(t *testing.T, st *store.Store, m store.Mute) store.Mute {
	t.Helper()
	if m.Reason == "" {
		m.Reason = "migration tonight"
	}
	m.MutedBy = "alice@example.com"
	out, err := st.CreateMute(context.Background(), m)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestMuteStartAndEnd(t *testing.T) {
	e := newEnv(t, channel("ops"))
	e.step(time.Minute)
	expect(t, "fires", e.step(time.Minute, f(quorum, "APP/order-jobs", "critical"))["ops"], "firing "+jobs)
	// Created between two epochs: it applies from the next epoch's time.
	m := mute(t, e.st, store.Mute{CheckCode: quorum, EntityType: "stream", EntityKey: "APP/order-jobs",
		CreatedAt: e.at.Add(30 * time.Second), ExpiresAt: e.at.Add(10 * time.Hour)})
	got := e.step(time.Minute, f(quorum, "APP/order-jobs", "critical"))
	expect(t, "muted", got["ops"], "muted "+jobs)
	if mu := got["ops"][0].Mute; mu == nil || mu.Accepted || mu.By != "alice@example.com" || mu.Reason != "migration tonight" ||
		!mu.Until.Equal(m.ExpiresAt) {
		t.Errorf("mute %+v", mu)
	}
	expect(t, "severity changes while muted", e.step(time.Minute, f(quorum, "APP/order-jobs", "warning"))["ops"])
	if _, err := e.st.UnmuteMute(context.Background(), m.ID, "bob@example.com", e.at.Add(10*time.Second)); err != nil {
		t.Fatal(err)
	}
	got = e.step(time.Minute, f(quorum, "APP/order-jobs", "warning"))
	expect(t, "unmuted", got["ops"], "firing "+jobs+" (mute ended)")
	if mu := got["ops"][0].Mute; mu == nil || mu.By != "alice@example.com" || got["ops"][0].Severity != "warning" {
		t.Errorf("mute ended: %+v", got["ops"][0])
	}
	expect(t, "next epoch", e.step(time.Minute, f(quorum, "APP/order-jobs", "warning"))["ops"])
	// A mute of the whole check that runs out.
	mute(t, e.st, store.Mute{CheckCode: quorum, CreatedAt: e.at, ExpiresAt: e.at.Add(90 * time.Second)})
	expect(t, "check muted", e.step(time.Minute, f(quorum, "APP/order-jobs", "warning"))["ops"], "muted "+jobs)
	expect(t, "check mute expired", e.step(time.Minute, f(quorum, "APP/order-jobs", "warning"))["ops"], "firing "+jobs+" (mute ended)")
}

// A finding muted for its whole episode sends nothing, not even its
// resolve; one whose mute ended long before it fired names no mute.
func TestMutedWholeEpisode(t *testing.T) {
	e := newEnv(t, channel("ops"))
	e.step(time.Minute)
	mute(t, e.st, store.Mute{CheckCode: quorum, CreatedAt: e.at, ExpiresAt: e.at.Add(5 * time.Minute)})
	expect(t, "fires muted", e.step(time.Minute, f(quorum, "APP/order-jobs", "critical"))["ops"])
	expect(t, "changes muted", e.step(time.Minute, f(quorum, "APP/order-jobs", "warning"))["ops"])
	expect(t, "resolves muted", e.step(time.Minute)["ops"])
	expect(t, "mute expires", e.step(time.Minute)["ops"])
	expect(t, "fires after", e.step(time.Hour, f(quorum, "APP/order-jobs", "critical"))["ops"], "firing "+jobs)
}

func TestAcceptanceEndedByReview(t *testing.T) {
	e := newEnv(t, channel("ops"))
	e.step(time.Minute)
	g := 2.0 * (1 << 20)
	a := mute(t, e.st, store.Mute{CheckCode: quorum, EntityType: "stream", EntityKey: "APP/order-jobs", Accepted: true,
		Reason: "by design", CreatedAt: e.at, ExpiresAt: e.at.Add(30 * 24 * time.Hour), GrowthPerDay: &g})
	expect(t, "fires accepted", e.step(time.Minute, f(quorum, "APP/order-jobs", "warning"))["ops"])
	e.apply(time.Minute, f(quorum, "APP/order-jobs", "warning"))
	// The review ends it at the epoch's time, before the reconcile.
	if ok, err := e.st.EndAcceptance(context.Background(), a.ID, e.at, "nats-lens", "growth changed from +2.0 MiB/day to +6.0 MiB/day"); err != nil || !ok {
		t.Fatalf("end acceptance: %v %v", ok, err)
	}
	got := e.reconcile()
	expect(t, "acceptance ended", got["ops"], "firing "+jobs+" (acceptance ended)")
	if mu := got["ops"][0].Mute; mu == nil || !mu.Accepted || mu.EndReason != "growth changed from +2.0 MiB/day to +6.0 MiB/day" ||
		mu.Reason != "by design" {
		t.Errorf("acceptance %+v", mu)
	}
}

// One flapping message, then nothing while it flaps, then the difference
// once it stops.
func TestFlapping(t *testing.T) {
	e := newEnv(t, channel("ops"))
	e.step(time.Minute)
	var all []string
	for i := range 4 {
		all = append(all, kinds(e.step(time.Minute, f(quorum, "APP/order-jobs", "critical"))["ops"])...)
		all = append(all, kinds(e.step(time.Minute)["ops"])...)
		_ = i
	}
	// The fourth resolve makes it flap: its message is the flapping one.
	want := []string{"firing " + jobs, "resolved " + jobs, "firing " + jobs, "resolved " + jobs, "firing " + jobs, "resolved " + jobs,
		"firing " + jobs, "flapping " + jobs}
	if !slices.Equal(all, want) {
		t.Fatalf("cycles:\n got  %q\n want %q", all, want)
	}
	for range 3 {
		expect(t, "fires while flapping", e.step(time.Minute, f(quorum, "APP/order-jobs", "warning"))["ops"])
		expect(t, "resolves while flapping", e.step(time.Minute)["ops"])
	}
	expect(t, "fires while flapping", e.step(time.Minute, f(quorum, "APP/order-jobs", "critical"))["ops"])
	// An hour later the cycles have aged out: the finding fires steadily.
	got := e.step(61*time.Minute, f(quorum, "APP/order-jobs", "critical"))
	expect(t, "stops flapping", got["ops"], "firing "+jobs+" (flapping ended)")
	if got["ops"][0].Flapping {
		t.Errorf("message after flapping says it flaps: %+v", got["ops"][0])
	}
	expect(t, "steady", e.step(time.Minute, f(quorum, "APP/order-jobs", "critical"))["ops"])
	expect(t, "resolves", e.step(time.Minute)["ops"], "resolved "+jobs)
}

// A flapping finding that stops flapping while resolved is sent its
// resolve.
func TestFlappingEndsResolved(t *testing.T) {
	e := newEnv(t, channel("ops"))
	e.step(time.Minute)
	for range 4 {
		e.step(time.Minute, f(quorum, "APP/order-jobs", "critical"))
		e.step(time.Minute)
	}
	expect(t, "an hour later", e.step(61*time.Minute)["ops"], "resolved "+jobs)
	if n := stateRows(t, e.st); n != 0 {
		t.Errorf("%d state rows", n)
	}
}

func TestReminders(t *testing.T) {
	e := newEnv(t, channel("ops", func(c *config.Channel) { c.RepeatEvery = config.Duration(time.Hour) }), channel("quiet"))
	e.step(time.Minute)
	expect(t, "fires", e.step(time.Minute, f(quorum, "APP/order-jobs", "critical"))["ops"], "firing "+jobs)
	for i := range 2 {
		got := e.step(20*time.Minute, f(quorum, "APP/order-jobs", "critical"))
		expect(t, fmt.Sprintf("%d minutes", 20*(i+1)), got["ops"])
	}
	got := e.step(20*time.Minute, f(quorum, "APP/order-jobs", "critical"))
	expect(t, "an hour", got["ops"], "reminder "+jobs)
	expect(t, "no reminders without repeat_every", got["quiet"])
	expect(t, "after the reminder", e.step(59*time.Minute, f(quorum, "APP/order-jobs", "critical"))["ops"])
	expect(t, "an hour after the reminder", e.step(time.Minute, f(quorum, "APP/order-jobs", "critical"))["ops"], "reminder "+jobs)
	// A change restarts the wait.
	e.step(30*time.Minute, f(quorum, "APP/order-jobs", "warning"))
	expect(t, "30 minutes after a change", e.step(40*time.Minute, f(quorum, "APP/order-jobs", "warning"))["ops"])
	expect(t, "an hour after a change", e.step(20*time.Minute, f(quorum, "APP/order-jobs", "warning"))["ops"], "reminder "+jobs)
}

func TestChannelFilters(t *testing.T) {
	e := newEnv(t,
		channel("all", func(c *config.Channel) { c.MinSeverity = "info" }),
		channel("critical", func(c *config.Channel) { c.MinSeverity = "critical" }),
		channel("quorum", func(c *config.Channel) { c.Checks = []string{quorum} }),
		channel("not-quorum", func(c *config.Channel) { c.ExcludeChecks = []string{quorum} }),
		channel("agreement", func(c *config.Channel) { c.Categories = []string{"agreement"} }),
		channel("staging", func(c *config.Channel) { c.Systems = []string{"example-staging"} }),
		channel("prod", func(c *config.Channel) { c.Systems = []string{"example-prod"} }),
		channel("firing-only", func(c *config.Channel) { c.Kinds = []string{"firing"} }),
	)
	got := e.step(time.Minute)
	if _, ok := got["staging"]; ok {
		t.Errorf("a channel of another system got %q", kinds(got["staging"]))
	}
	for _, c := range []string{"all", "critical", "quorum", "not-quorum", "agreement", "prod", "firing-only"} {
		expect(t, c+" baseline", got[c], "baseline")
	}
	fires := []fire{f(quorum, "APP/order-jobs", "critical"), f(quorum, "APP/images", "info"), f(version, "nats-1", "warning")}
	got = e.step(time.Minute, fires...)
	const ver = "server-005:nats-1"
	expect(t, "all", got["all"], "firing "+jobs, "firing "+ver, "firing "+images)
	expect(t, "critical", got["critical"], "firing "+jobs)
	expect(t, "quorum", got["quorum"], "firing "+jobs)
	expect(t, "not-quorum", got["not-quorum"], "firing "+ver)
	expect(t, "agreement", got["agreement"], "firing "+ver)
	expect(t, "prod", got["prod"], "firing "+jobs, "firing "+ver)
	expect(t, "staging", got["staging"])
	got = e.step(time.Minute)
	expect(t, "all resolves", got["all"], "resolved "+jobs, "resolved "+ver, "resolved "+images)
	expect(t, "firing-only takes no resolve", got["firing-only"])
	if n := stateRows(t, e.st); n != 0 {
		t.Errorf("%d state rows after every resolve: a kind left out still moves the state", n)
	}
	expect(t, "firing-only fires again", e.step(time.Minute, fires...)["firing-only"], "firing "+jobs, "firing "+ver)
}

// A channel whose filter no longer takes a finding it was told about (the
// file changed between two runs) is sent its resolve.
func TestFilterChangedAcrossRestart(t *testing.T) {
	e := newEnv(t, channel("ops"))
	e.step(time.Minute)
	expect(t, "fires", e.step(time.Minute, f(quorum, "APP/order-jobs", "critical"))["ops"], "firing "+jobs)
	e.rec.Channels = []config.Channel{channel("ops", func(c *config.Channel) { c.ExcludeChecks = []string{quorum} })}
	expect(t, "excluded", e.step(time.Minute, f(quorum, "APP/order-jobs", "critical"))["ops"],
		"resolved "+jobs+" (no longer selected by the channel)")
}

// A channel added later gets the baseline only, counting what fires in
// its view, and then the differences.
func TestChannelAddedLater(t *testing.T) {
	e := newEnv(t, channel("ops"))
	e.step(time.Minute)
	e.step(time.Minute, f(quorum, "APP/order-jobs", "critical"), f(quorum, "APP/images", "warning"))
	e.rec.Channels = append(e.rec.Channels, channel("chat", func(c *config.Channel) { c.MinSeverity = "critical" }))
	got := e.step(time.Minute, f(quorum, "APP/order-jobs", "warning"), f(quorum, "APP/images", "warning"), f(version, "nats-1", "critical"))
	expect(t, "old channel", got["ops"], "firing server-005:nats-1", "changed "+jobs)
	expect(t, "new channel", got["chat"], "baseline")
	if b := got["chat"][0]; *b.FiringCount != 1 || b.Detail != "nats-lens now notifies this channel: 1 finding firing" {
		t.Errorf("baseline %+v", b)
	}
	got = e.step(time.Minute, f(quorum, "APP/order-jobs", "critical"), f(quorum, "APP/images", "warning"))
	expect(t, "new channel then", got["chat"], "firing "+jobs, "resolved server-005:nats-1")
}

// The notifier stopped for 10 epochs (natslens once, or a stop) and then
// run once sends the net difference.
func TestNetDifferenceAfterAGap(t *testing.T) {
	e := newEnv(t, channel("ops"))
	e.step(time.Minute)
	got := e.step(time.Minute, f(quorum, "APP/order-jobs", "critical"), f(quorum, "APP/images", "warning"), f(version, "nats-1", "warning"))
	expect(t, "before", got["ops"], "firing "+jobs, "firing server-005:nats-1", "firing "+images)
	// order-jobs: changes twice and ends as it was; images: resolves and
	// fires again; nats-1: resolves; nats-2: fires and resolves;
	// nats-3: fires and stays.
	e.apply(time.Minute, f(quorum, "APP/order-jobs", "warning"), f(quorum, "APP/images", "warning"), f(version, "nats-1", "warning"))
	e.apply(time.Minute, f(quorum, "APP/order-jobs", "critical"), f(version, "nats-1", "warning"), f(version, "nats-2", "warning"))
	e.apply(time.Minute, f(quorum, "APP/order-jobs", "critical"), f(quorum, "APP/images", "critical"))
	for range 7 {
		e.apply(time.Minute, f(quorum, "APP/order-jobs", "critical"), f(quorum, "APP/images", "critical"), f(version, "nats-3", "warning"))
	}
	got = e.step(time.Minute, f(quorum, "APP/order-jobs", "critical"), f(quorum, "APP/images", "critical"), f(version, "nats-3", "warning"))
	expect(t, "after 10 epochs", got["ops"], "resolved "+images, "firing "+images, "firing server-005:nats-3", "resolved server-005:nats-1")
	if r := got["ops"][0]; r.Severity != "warning" || r.DedupKey != "example-prod:"+images+":2:resolved:13" {
		t.Errorf("resolve of the old episode %+v", r)
	}
	if r := got["ops"][1]; r.DedupKey != "example-prod:"+images+":5:firing:13" {
		t.Errorf("firing of the new episode %+v", r)
	}
}

// Messages of one epoch go worst first: critical, then warnings with an
// expiry warning last, then mutes, then resolves.
func TestOrderWorstFirst(t *testing.T) {
	e := newEnv(t, channel("ops"))
	e.step(time.Minute)
	e.step(time.Minute, f(quorum, "APP/a", "warning"), f(quorum, "APP/b", "warning"))
	mute(t, e.st, store.Mute{CheckCode: quorum, EntityType: "stream", EntityKey: "APP/b", CreatedAt: e.at, ExpiresAt: e.at.Add(time.Hour)})
	got := e.step(time.Minute, f(quorum, "APP/b", "warning"), f("server-016", "nats-0", "warning"), f(version, "nats-1", "warning"),
		f(quorum, "APP/c", "critical"))
	if !checks.IsExpiry("server-016") {
		t.Fatal("server-016 is no expiry check")
	}
	expect(t, "order", got["ops"], "firing stream-001:APP/c", "firing server-005:nats-1", "firing server-016:nats-0",
		"muted stream-001:APP/b", "resolved stream-001:APP/a")
}

// The state persists: a reopened store continues where the last process
// stopped, without a baseline and without sending again.
func TestRestartKeepsState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.duckdb")
	open := func() *store.Store {
		st, err := store.OpenPath(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		return st
	}
	st := open()
	e := envOn(t, st, "example-prod", channel("ops"))
	e.step(time.Minute)
	expect(t, "fires", e.step(time.Minute, f(quorum, "APP/order-jobs", "critical"))["ops"], "firing "+jobs)
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st = open()
	t.Cleanup(func() { _ = st.Close() })
	e2 := envOn(t, st, "example-prod", channel("ops"))
	e2.epoch, e2.at, e2.seen = e.epoch, e.at, e.seen
	expect(t, "after the restart", e2.step(time.Minute, f(quorum, "APP/order-jobs", "critical"))["ops"])
	expect(t, "changes after the restart", e2.step(time.Minute, f(quorum, "APP/order-jobs", "warning"))["ops"], "changed "+jobs)
	pending, err := st.PendingOutbox(context.Background(), "ops", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 3 || pending[0].Kind != "baseline" || pending[1].Kind != "firing" || pending[2].Kind != "changed" ||
		pending[0].ID >= pending[1].ID || pending[1].ID >= pending[2].ID {
		t.Errorf("pending after a restart %+v", pending)
	}
}

// An epoch reconciled twice queues its messages once.
func TestReconcileTwice(t *testing.T) {
	e := newEnv(t, channel("ops"))
	e.step(time.Minute)
	expect(t, "fires", e.step(time.Minute, f(quorum, "APP/order-jobs", "critical"))["ops"], "firing "+jobs)
	expect(t, "again", e.reconcile()["ops"])
	if err := e.st.ApplyNotify(context.Background(), store.NotifyChange{Channels: []store.NotifyChannel{{Name: "ops",
		ReconciledEpoch: e.epoch}}}); err == nil || !strings.Contains(err.Error(), "already reconciled") {
		t.Errorf("a second write for the epoch: %v", err)
	}
}

// Each system has its own store and its own state; a channel's filter
// takes the systems it names.
func TestMultipleSystems(t *testing.T) {
	chans := []config.Channel{channel("ops"), channel("prod-only", func(c *config.Channel) { c.Systems = []string{"example-prod"} })}
	prod := newEnv(t, chans...)
	stg, err := store.OpenPath(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stg.Close() })
	staging := envOn(t, stg, "example-staging", chans...)
	prod.step(time.Minute)
	got := staging.step(time.Minute)
	expect(t, "staging baseline", got["ops"], "baseline")
	expect(t, "staging prod-only", got["prod-only"])
	p := prod.step(time.Minute, f(quorum, "APP/order-jobs", "critical"))
	s := staging.step(time.Minute, f(quorum, "APP/order-jobs", "critical"))
	expect(t, "prod ops", p["ops"], "firing "+jobs)
	expect(t, "prod prod-only", p["prod-only"], "firing "+jobs)
	expect(t, "staging ops", s["ops"], "firing "+jobs)
	expect(t, "staging prod-only", s["prod-only"])
	if p["ops"][0].DedupKey == s["ops"][0].DedupKey || !strings.HasPrefix(s["ops"][0].DedupKey, "example-staging:") {
		t.Errorf("dedup keys %s and %s", p["ops"][0].DedupKey, s["ops"][0].DedupKey)
	}
}

// An entity is named as the UI names it: its account by readable name.
func TestEntityNames(t *testing.T) {
	e := newEnv(t, channel("ops"))
	e.step(time.Minute)
	if _, err := e.st.DB().Exec(`INSERT INTO accounts VALUES ('APP', 'shop', false, 1, 1), ('SYS', 'SYS', true, 1, 1)`); err != nil {
		t.Fatal(err)
	}
	got := e.step(time.Minute, f(quorum, "APP/order-jobs", "critical"), f(consumer, "APP/order-jobs/resize", "critical"),
		f(quorum, "SYS/x", "critical"), f(version, "nats-1", "critical"))
	names := map[string]string{}
	for _, m := range got["ops"] {
		names[m.EntityKey] = m.EntityName
	}
	want := map[string]string{"APP/order-jobs": "shop/order-jobs", "APP/order-jobs/resize": "shop/order-jobs/resize", "SYS/x": "", "nats-1": ""}
	for k, v := range want {
		if names[k] != v {
			t.Errorf("%s named %q, want %q", k, names[k], v)
		}
	}
}

// A store rebuilt with findings but without notification state (a lost
// database rebuilt from the archive) gives every channel its baseline
// and nothing else.
func TestRebuiltStoreBaselines(t *testing.T) {
	e := newEnv(t, channel("ops"))
	e.step(time.Minute)
	e.step(time.Minute, f(quorum, "APP/order-jobs", "critical"), f(quorum, "APP/images", "warning"), f(quorum, "APP/info", "info"))
	rb := rebuild(t, e.st, e.at)
	r := envOn(t, rb, "example-prod", channel("ops"))
	r.epoch, r.at = e.epoch, e.at
	got := r.step(time.Minute, f(quorum, "APP/order-jobs", "critical"), f(quorum, "APP/images", "warning"), f(quorum, "APP/info", "info"))
	expect(t, "after the rebuild", got["ops"], "baseline")
	if b := got["ops"][0]; *b.FiringCount != 2 {
		t.Errorf("baseline counts %d", *b.FiringCount)
	}
	expect(t, "next epoch", r.step(time.Minute, f(quorum, "APP/order-jobs", "critical"))["ops"], "resolved "+images)
}

// 3,000 findings that fire at once are reconciled in one transaction.
func TestManyFindingsAtOnce(t *testing.T) {
	e := newEnv(t, channel("ops"), channel("chat", func(c *config.Channel) { c.MinSeverity = "critical" }))
	e.step(time.Minute)
	var fires []fire
	for i := range 3000 {
		sev := "warning"
		if i%3 == 0 {
			sev = "critical"
		}
		fires = append(fires, f(quorum, fmt.Sprintf("APP/s-%04d", i), sev))
	}
	e.apply(time.Minute, fires...)
	start := time.Now()
	got := e.reconcile()
	took := time.Since(start)
	t.Logf("3,000 findings, 2 channels: reconciled in %s", took)
	if len(got["ops"]) != 3000 || len(got["chat"]) != 1000 {
		t.Fatalf("queued %d and %d", len(got["ops"]), len(got["chat"]))
	}
	for i, m := range got["ops"] {
		if (i < 1000) != (m.Severity == "critical") {
			t.Fatalf("message %d is %s: not worst first", i, m.Severity)
		}
	}
	if n := stateRows(t, e.st); n != 4000 {
		t.Errorf("%d state rows", n)
	}
	start = time.Now()
	got = e.step(time.Minute)
	t.Logf("3,000 resolves: applied and reconciled in %s", time.Since(start))
	if len(got["ops"]) != 3000 || stateRows(t, e.st) != 0 {
		t.Errorf("resolves %d, state rows %d", len(got["ops"]), stateRows(t, e.st))
	}
}

func TestSweepNotifications(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, channel("ops"), channel("old"))
	e.step(time.Minute)
	e.step(time.Minute, f(quorum, "APP/order-jobs", "critical"))
	if _, err := e.st.DB().Exec(`UPDATE notify_outbox SET state = 'delivered' WHERE kind = 'baseline'`); err != nil {
		t.Fatal(err)
	}
	e.rec.Channels = e.rec.Channels[:1]
	if err := e.rec.Sweep(ctx, e.at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	count := func(q string) int {
		var n int
		if err := e.st.DB().QueryRow(q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count(`SELECT count(*) FROM notify_outbox WHERE channel = 'old'`) + count(`SELECT count(*) FROM notify_state WHERE channel = 'old'`) +
		count(`SELECT count(*) FROM notify_channels WHERE channel = 'old'`); n != 0 {
		t.Errorf("%d rows of a removed channel", n)
	}
	if n := count(`SELECT count(*) FROM notify_outbox`); n != 2 {
		t.Errorf("%d outbox rows of ops kept before their age, want 2", n)
	}
	// A day of retention keeps a delivered row a day, a pending one for
	// ever.
	e.rec.Retention = 24 * time.Hour
	if err := e.rec.Sweep(ctx, e.at.Add(25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if n := count(`SELECT count(*) FROM notify_outbox WHERE kind = 'baseline'`); n != 0 {
		t.Errorf("delivered row older than retention kept")
	}
	if n := count(`SELECT count(*) FROM notify_outbox WHERE state = 'pending'`); n != 1 {
		t.Errorf("%d pending rows, want 1", n)
	}
	// The state of a finding the retention sweep removed goes with it.
	if _, err := e.st.DB().Exec(`DELETE FROM findings`); err != nil {
		t.Fatal(err)
	}
	if err := e.rec.Sweep(ctx, e.at.Add(25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if n := stateRows(t, e.st); n != 0 {
		t.Errorf("%d state rows of a removed finding", n)
	}
}

// A finding the store lost while the channel knew it is sent a resolve
// built from what the channel was told.
func TestFindingGoneFromStore(t *testing.T) {
	e := newEnv(t, channel("ops"))
	e.step(time.Minute)
	e.step(time.Minute, f(quorum, "APP/order-jobs", "critical"))
	if _, err := e.st.DB().Exec(`DELETE FROM findings`); err != nil {
		t.Fatal(err)
	}
	got := e.step(time.Minute)
	expect(t, "gone", got["ops"], "resolved "+jobs)
	if m := got["ops"][0]; m.CheckCode != quorum || m.CheckName != "Stream quorum lost" || m.EntityKey != "APP/order-jobs" || m.Severity != "critical" {
		t.Errorf("resolve of a lost finding %+v", m)
	}
}
