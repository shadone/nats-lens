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
		if r.Lang == "" || r.Version == "" || r.Version == "0.0.0" {
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
	// Compute shared context once, independent of application cardinality.
	counts, unobserved := b.counts()
	var contextEvidence []*v1.ReplayEvidence
	firings := 0
	for _, t := range b.f.Transitions {
		if b.inRange(t.Time) && t.State == v1.FindingState_FINDING_STATE_FIRING && t.Severity >= v1.Severity_SEVERITY_WARNING {
			firings++
			// Evidence beyond this cap is still counted by the normal finding steps.
			if len(contextEvidence) < maxStepEvidence-2 {
				contextEvidence = append(contextEvidence, findingEvidence(t))
			}
		}
	}
	var absent, unnamed, unknown int
	for _, k := range keys {
		g := groups[k]
		if k.app == UnnamedApplication {
			unnamed++
			continue
		}
		if len(g.a) == 0 || len(g.z) == 0 {
			absent++
			continue
		}
		if g.unknown {
			unknown++
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
			body:     fmt.Sprintf("Account %s. At A: %s. At B: %s. Counts are connections; complete CONNZ at both endpoints. This spans observations, not a change time. This is not evidence of an application deployment or release version. Identity is the name before @ within this account; names can be reused.", k.account, mixText(g.a), mixText(g.z)),
			evidence: []*v1.ReplayEvidence{collectionEvidence(b.f.EpochA, b.f.EpochAAt, ref), collectionEvidence(b.f.EpochB, b.f.EpochBAt, ref)}}
		// Reuse Replay's exact count availability and reasons, including capture gaps.
		if counts.ClientReconnects != nil {
			pairWord := "pairs"
			if counts.GetClientReconnects() == 1 {
				pairWord = "pair"
			}
			d.body += fmt.Sprintf(" Same A–B window, system-wide: %d reconnect %s (client-004 rules, including server shutdowns).", counts.GetClientReconnects(), pairWord)
		} else {
			reason := "not observed"
			for _, u := range unobserved {
				if u.Count == "client_reconnects" {
					switch u.Reason {
					case v1.ReplayUnobservedReason_REPLAY_UNOBSERVED_REASON_EVENTS_NOT_CAPTURED:
						reason = "no events captured"
					case v1.ReplayUnobservedReason_REPLAY_UNOBSERVED_REASON_COLLECTOR_GAP:
						reason = "collector downtime"
					case v1.ReplayUnobservedReason_REPLAY_UNOBSERVED_REASON_EVENTS_CONNECTION_LOST:
						reason = "event connection lost"
					}
				}
			}
			d.body += " Same A–B window, system-wide reconnect pairs unavailable: " + reason + "."
		}
		d.evidence = append(d.evidence, contextEvidence...)
		if b.f.TransitionsTruncated {
			d.body += fmt.Sprintf(" %d warning/critical finding firings read in that system-wide window; the finding read was truncated.", firings)
		} else {
			d.body += fmt.Sprintf(" %d warning/critical finding firings in that system-wide window.", firings)
		}
		d.body += " These share a time window, not a causal link or application attribution. See separate server-upgrade, Kubernetes and gap steps; intervening activity may be missed. Up to eight finding links are included here."
		b.add(d)
	}
	if absent+unnamed+unknown > 0 {
		b.add(&draft{kind: kind, time: b.f.A, end: b.f.B, title: "Client-library comparison limits", body: fmt.Sprintf("Application/account groups not compared: %d absent at one endpoint, %d unnamed, %d with unknown library metadata (empty or 0.0.0). New or renamed applications do not prove deployments. Unchanged library sets do not establish that no deployment occurred.", absent, unnamed, unknown), evidence: []*v1.ReplayEvidence{collectionEvidence(b.f.EpochA, b.f.EpochAAt, nil), collectionEvidence(b.f.EpochB, b.f.EpochBAt, nil)}})
	}
}
