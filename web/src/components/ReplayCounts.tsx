// "Between A and B": what changed, in numbers. Observed counts are tiles,
// a zero one quieter; the counts that were not observed are named, with
// the reason when the response gives one, never shown as 0.
import type { ReplayCounts as Counts, ReplayStep } from "../api/adapt-replay";
import { fmtInt, hm } from "../lib/format";
import { countTiles, stepWhen, type CountSource, type CountTile } from "../lib/replayView";

function lower(label: string): string {
  return label.charAt(0).toLowerCase() + label.slice(1);
}

/** "a, b and c" */
function listText(items: string[]): string {
  return items.length < 2 ? items.join("") : `${items.slice(0, -1).join(", ")} and ${items[items.length - 1]}`;
}

export type GapKind = "collector" | "kubernetes" | "collections";

/**
 * The kind of a gap step, from its title as the API words it: "nats-lens
 * was not running …", "Kubernetes was not watched …", "No collections …".
 * The response has no field for it; another title gives undefined.
 */
export function gapKind(s: ReplayStep): GapKind | undefined {
  if (/^nats-lens (was )?not running/.test(s.title)) return "collector";
  if (/^Kubernetes (was )?not watched/.test(s.title)) return "kubernetes";
  if (/^No collections/.test(s.title)) return "collections";
  return undefined;
}

/** The counts each gap kind can leave unset, as the API unsets them. */
const GAP_SOURCES: Record<GapKind, CountSource[]> = {
  kubernetes: ["node"],
  collector: ["event", "node"],
  collections: ["server", "gone"],
};

/** Counts the read limit can leave unset: those taken from events and transitions. */
const LIMIT_SOURCES: CountSource[] = ["event", "finding"];

interface Reason {
  text: (names: string) => string;
  sources: CountSource[];
}

export function ReplayCounts({
  counts,
  kubernetesWatched,
  gaps = [],
  completeUntil,
  fmt = hm,
}: {
  counts: Counts;
  kubernetesWatched: boolean;
  /** The gap steps of the response. */
  gaps?: ReplayStep[];
  /** When the read limit cut the events or transitions. */
  completeUntil?: number;
  fmt?: (t: number) => string;
}) {
  const tiles = countTiles(counts);
  const seen = tiles.filter((t) => t.value !== undefined);
  const unseen = tiles.filter((t) => t.value === undefined);
  const names = (ts: CountTile[]) => listText(ts.map((t) => lower(t.label)));

  // Each unset count goes to the first reason that can explain it; one
  // without a reason is only named.
  const reasons: Reason[] = [];
  if (!kubernetesWatched) reasons.push({ sources: ["node"], text: (n) => `Kubernetes is not watched for this system, so ${n} are not counted.` });
  const byKind = (k: GapKind) => gaps.filter((g) => gapKind(g) === k);
  const times = (gs: ReplayStep[]) => listText(gs.map((g) => stepWhen(g, fmt)));
  const had = (gs: ReplayStep[]) => (gs.length === 1 ? "had a gap" : "had gaps");
  const k8s = byKind("kubernetes");
  if (k8s.length) reasons.push({ sources: GAP_SOURCES.kubernetes, text: (n) => `The Kubernetes watch ${had(k8s)} (${times(k8s)}), which can leave ${n} not observed.` });
  const collector = byKind("collector");
  if (collector.length) reasons.push({ sources: GAP_SOURCES.collector, text: (n) => `nats-lens was not running (${times(collector)}), which can leave ${n} not observed.` });
  const collections = byKind("collections");
  if (collections.length) reasons.push({ sources: GAP_SOURCES.collections, text: (n) => `Collections ${had(collections)} (${times(collections)}), which can leave ${n} not observed.` });
  if (completeUntil !== undefined) reasons.push({ sources: LIMIT_SOURCES, text: (n) => `The read limit was reached at ${hm(completeUntil)}, which can leave ${n} not observed.` });

  let left = unseen;
  const explained: string[] = [];
  for (const r of reasons) {
    const mine = left.filter((t) => r.sources.includes(t.source));
    if (!mine.length) continue;
    explained.push(r.text(names(mine)));
    left = left.filter((t) => !mine.includes(t));
  }

  return (
    <section className="sec rp-counts">
      <div className="sec-h">
        <h2>Between A and B</h2>
      </div>
      <div className="card rp-counts-card">
        {seen.length > 0 && (
          <ul className="rp-tiles" aria-label="Between A and B">
            {seen.map((t) => (
              <li key={t.label} className={`rp-tile${t.value === 0 ? " zero" : t.tone ? ` t-${t.tone}` : ""}`}>
                <span className="rp-tv">{fmtInt(t.value!)}</span>
                <span className="rp-tl">{t.label}</span>
                {t.note && <small>{t.note}</small>}
              </li>
            ))}
          </ul>
        )}
        {left.length > 0 && (
          <p className="rp-unseen">
            <b>Not observed:</b> {left.map((t) => lower(t.label)).join(", ")}.
          </p>
        )}
        {explained.map((text) => (
          <p key={text} className="rp-unseen">
            {text}
          </p>
        ))}
      </div>
    </section>
  );
}
