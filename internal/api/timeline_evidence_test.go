package api

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func TestWindowEvidenceRetainsExactDerivationInputs(t *testing.T) {
	at := time.Date(2026, 10, 17, 23, 59, 0, 0, time.UTC)
	rows := []K8sWindowEvent{
		{ID: "9007199254740993", At: at, Kind: "k8s.node.drained", Node: "node-a"},
		{ID: "9007199254740994", At: at.Add(2 * time.Minute), Kind: "k8s.pod.killing", Server: "nats-0", Node: "node-a"},
		{ID: "9007199254740995", At: at.Add(5 * time.Minute), Kind: "k8s.pod.ready", Server: "nats-0", Node: "node-a"},
		{ID: "9007199254740996", At: at.Add(time.Hour), Kind: "k8s.node.ready", Node: "unrelated"},
	}
	windows := DeriveK8sWindows(rows, K8sUpgradeGap)
	if len(windows) != 1 {
		t.Fatalf("windows: %v", windows)
	}
	w := windows[0]
	if len(w.Id) != 64 || !slices.Equal(w.SupportingEventIds, []string{"9007199254740993", "9007199254740994", "9007199254740995"}) || w.EvidenceTotal != 3 || !strings.Contains(w.Derivation, "15") {
		t.Fatalf("derivation evidence missing or broadened: %v", w)
	}
	if !w.Start.AsTime().Equal(at) || !w.End.AsTime().Equal(at.Add(5*time.Minute)) {
		t.Fatal("observed bounds changed")
	}
	if again := DeriveK8sWindows(rows, K8sUpgradeGap)[0]; again.Id != w.Id {
		t.Fatal("unstable window identity")
	}
	rows[1].ID = "9007199254740997"
	if changed := DeriveK8sWindows(rows, K8sUpgradeGap)[0]; changed.Id == w.Id {
		t.Fatal("changed retained inputs silently retained identity")
	}
	rows[1].ID = ""
	if missing := DeriveK8sWindows(rows, K8sUpgradeGap)[0]; !slices.Contains(missing.Unavailable, "supporting_event_identity") {
		t.Fatalf("missing identity invented: %v", missing)
	}
}

func TestEventSelectionValidatesTypedIdentityBudget(t *testing.T) {
	good, err := EventSelection([]string{"9007199254740993", "9223372036854775807"}, nil)
	if err != nil || !slices.Equal(good, []int64{9007199254740993, 9223372036854775807}) {
		t.Fatalf("precision lost: %v %v", good, err)
	}
	for _, ids := range [][]string{{"0"}, {"-1"}, {"01"}, {"+1"}, {"1e3"}, {"9223372036854775808"}, {"1", "1"}, {"synthetic-sensitive-text"}, make([]string, 1001)} {
		if _, err := EventSelection(ids, nil); err == nil || strings.Contains(err.Error(), "synthetic-sensitive-text") {
			t.Fatalf("unsafe selection admitted/disclosed: %v", err)
		}
	}
	negative := int64(-1)
	if _, err := EventSelection(nil, &negative); err == nil {
		t.Fatal("negative boundary admitted")
	}
	zero := int64(0)
	if _, err := EventSelection(nil, &zero); err != nil {
		t.Fatal("known empty boundary rejected", err)
	}
}
