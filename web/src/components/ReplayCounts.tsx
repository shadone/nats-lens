// "Between A and B": what changed, in numbers. Observed counts are tiles,
// a zero one quieter; the counts that were not observed are named in one
// line, never shown as 0.
import type { ReplayCounts as Counts } from "../api/adapt-replay";
import { fmtInt } from "../lib/format";
import { hm } from "../lib/format";
import { countTiles, stepWhen } from "../lib/replayView";
import type { ReplayStep } from "../api/adapt-replay";

function lower(label: string): string {
  return label.charAt(0).toLowerCase() + label.slice(1);
}

/** "a, b and c" */
function listText(items: string[]): string {
  return items.length < 2 ? items.join("") : `${items.slice(0, -1).join(", ")} and ${items[items.length - 1]}`;
}

export function ReplayCounts({ counts, kubernetesWatched, gaps = [], fmt = hm }: { counts: Counts; kubernetesWatched: boolean; gaps?: ReplayStep[]; fmt?: (t: number) => string }) {
  const tiles = countTiles(counts);
  const seen = tiles.filter((t) => t.value !== undefined);
  const unseen = tiles.filter((t) => t.value === undefined);
  // A reason for the counts that were not observed, when the response
  // gives one: no Kubernetes watch for the node counts; a gap, named by its
  // times (it may lie around A or B, not only between them), for the rest.
  const unwatched = kubernetesWatched ? [] : unseen.filter((t) => t.k8s);
  const left = unseen.filter((t) => !unwatched.includes(t));
  const inGap = gaps.length ? left : [];
  const rest = left.filter((t) => !inGap.includes(t));
  const names = (ts: typeof tiles) => listText(ts.map((t) => lower(t.label)));
  const gapTimes = listText(gaps.map((g) => stepWhen(g, fmt)));
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
        {rest.length > 0 && (
          <p className="rp-unseen">
            <b>Not observed:</b> {rest.map((t) => lower(t.label)).join(", ")}.
          </p>
        )}
        {unwatched.length > 0 && <p className="rp-unseen">Kubernetes is not watched for this system, so {names(unwatched)} are not counted.</p>}
        {inGap.length > 0 && (
          <p className="rp-unseen">
            Collections or event capture had a gap ({gapTimes}; see the gap {gaps.length === 1 ? "step" : "steps"}), which can leave {names(inGap)} not observed.
          </p>
        )}
      </div>
    </section>
  );
}
