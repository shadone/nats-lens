package notify

import (
	"strings"
	"testing"

	"github.com/shadone/nats-lens/internal/checks"
	"github.com/shadone/nats-lens/internal/config"
)

func TestCheckFilters(t *testing.T) {
	set, err := checks.Load(config.Checks{})
	if err != nil {
		t.Fatal(err)
	}
	ch := config.DefaultChannel()
	ch.Name = "ops-webhook"
	ch.Checks = []string{"stream-001"}
	ch.ExcludeChecks = []string{"system-001"}
	ch.Categories = []string{"health", "storage"}
	n := config.Notifications{Channels: []config.Channel{ch}}
	if err := CheckFilters(n, set); err != nil {
		t.Fatalf("known codes and categories refused: %v", err)
	}
	ch.Checks = []string{"stream-999"}
	ch.ExcludeChecks = []string{"server-998"}
	ch.Categories = []string{"latency"}
	n.Channels = []config.Channel{ch}
	err = CheckFilters(n, set)
	if err == nil {
		t.Fatal("unknown codes and category accepted")
	}
	for _, want := range []string{
		`notifications.channels[0] (ops-webhook): checks: unknown check "stream-999"`,
		`exclude_checks: unknown check "server-998"`,
		`categories: unknown category "latency"; categories are `,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %v", want, err)
		}
	}
}
