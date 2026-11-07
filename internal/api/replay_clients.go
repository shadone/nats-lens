package api

import (
	"fmt"
	"slices"
	"strings"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// ReplayClientLibraries is an endpoint comparison, not a deployment detector.
type ReplayClientLibraries struct {
	Rows        []ReplayClientLibrary
	Unavailable string
}

type ReplayClientLibrary struct {
	Epoch                       int64
	App, Account, Lang, Version string
	Connections                 int64
}

func (b *replayBuilder) clientLibraries() {
	c := b.f.ClientLibraries
	if c == nil {
		return
	}
	kind := v1.ReplayStepKind_REPLAY_STEP_KIND_CLIENT_CHANGE
	if c.Unavailable != "" {
		b.add(&draft{kind: kind, time: b.f.A, end: b.f.B, title: "Client-library comparison unavailable", body: c.Unavailable + " No application deployment is inferred.",
			evidence: []*v1.ReplayEvidence{collectionEvidence(b.f.EpochA, b.f.EpochAAt, nil), collectionEvidence(b.f.EpochB, b.f.EpochBAt, nil)}})
		return
	}
	type key struct{ app, account string }
	type mix struct {
		a, z    []ReplayClientLibrary
		unknown bool
	}
	groups := map[key]*mix{}
	for _, r := range c.Rows {
		k := key{r.App, r.Account}
		g := groups[k]
		if g == nil {
			g = &mix{}
			groups[k] = g
		}
		if r.Lang == "" || r.Version == "" || r.Version == "0.0.0" || r.App == UnnamedApplication {
			g.unknown = true
		}
		if r.Epoch == b.f.EpochA {
			g.a = append(g.a, r)
		} else if r.Epoch == b.f.EpochB {
			g.z = append(g.z, r)
		}
	}
	keys := slices.SortedFunc(mapKeys(groups), func(a, z key) int {
		if n := strings.Compare(a.app, z.app); n != 0 {
			return n
		}
		return strings.Compare(a.account, z.account)
	})
	skipped := 0
	for _, k := range keys {
		g := groups[k]
		if g.unknown || len(g.a) == 0 || len(g.z) == 0 {
			skipped++
			continue
		}
		set := func(rs []ReplayClientLibrary) []string {
			out := []string{}
			for _, r := range rs {
				out = append(out, r.Lang+" "+r.Version)
			}
			slices.Sort(out)
			return slices.Compact(out)
		}
		if slices.Equal(set(g.a), set(g.z)) {
			continue
		}
		mixText := func(rs []ReplayClientLibrary) string {
			parts := []string{}
			for _, r := range rs {
				parts = append(parts, fmt.Sprintf("%s %s (%d)", r.Lang, r.Version, r.Connections))
			}
			slices.Sort(parts)
			// A hostile client can report thousands of versions. Keep the wording bounded.
			if len(parts) > 10 {
				return strings.Join(parts[:10], ", ") + fmt.Sprintf("; %d more library versions", len(parts)-10)
			}
			return strings.Join(parts, ", ")
		}
		ref := &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_CLIENT, Key: k.app, Name: k.app, Account: k.account}
		d := &draft{kind: kind, time: b.f.EpochAAt, end: b.f.EpochBAt, title: k.app + " client-library set differs", entities: []*v1.EntityRef{ref},
			body:     fmt.Sprintf("Account %s. At A: %s. At B: %s. Counts are observed connections. Both collections have complete CONNZ coverage. This compares endpoint library sets, not the time of a change; intermediate versions may be missed. This is not evidence of an application deployment or release version. Application identity is the name before @ within this account; names can be reused.", k.account, mixText(g.a), mixText(g.z)),
			evidence: []*v1.ReplayEvidence{collectionEvidence(b.f.EpochA, b.f.EpochAAt, ref), collectionEvidence(b.f.EpochB, b.f.EpochBAt, ref)}}
		// Reuse Replay's exact count availability and reasons, including capture gaps.
		counts, unobserved := b.counts()
		if counts.ClientReconnects != nil {
			d.body += fmt.Sprintf(" Same A–B window, system-wide: %d reconnect pairs (client-004 rules, including server shutdowns).", counts.GetClientReconnects())
		} else {
			reason := "not observed"
			for _, u := range unobserved {
				if u.Count == "client_reconnects" {
					reason = u.Reason.String()
				}
			}
			d.body += " Same A–B window, system-wide reconnect pairs unavailable: " + reason + "."
		}
		n := 0
		for _, t := range b.f.Transitions {
			if b.inRange(t.Time) && t.State == v1.FindingState_FINDING_STATE_FIRING && t.Severity >= v1.Severity_SEVERITY_WARNING {
				n++
				d.evidence = append(d.evidence, findingEvidence(t))
			}
		}
		if b.f.TransitionsTruncated {
			d.body += fmt.Sprintf(" %d warning/critical finding firings read in that system-wide window; the finding read was truncated.", n)
		} else {
			d.body += fmt.Sprintf(" %d warning/critical finding firings in that system-wide window.", n)
		}
		d.body += " These share a bounded time window, not a causal link or application attribution. Server upgrades and Kubernetes observations remain separate steps. Collection or capture gaps can hide intervening activity."
		b.add(d)
	}
	if skipped > 0 {
		b.add(&draft{kind: kind, time: b.f.A, end: b.f.B, title: "Client-library comparison limits", body: fmt.Sprintf("%d application/account groups are not compared: absent at one endpoint, unnamed, or carrying unknown library metadata (empty or 0.0.0). New or renamed applications are not deployments inferred by Replay. Unchanged library sets do not establish that no deployment occurred.", skipped), evidence: []*v1.ReplayEvidence{collectionEvidence(b.f.EpochA, b.f.EpochAAt, nil), collectionEvidence(b.f.EpochB, b.f.EpochBAt, nil)}})
	}
}
