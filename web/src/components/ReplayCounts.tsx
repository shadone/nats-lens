// "Between A and B": what changed, in numbers. Observed counts are tiles,
// a zero one quieter; the counts that were not observed are named in one
// line, never shown as 0.
import type { ReplayCounts as Counts } from "../api/adapt-replay";
import { fmtInt } from "../lib/format";
import { countTiles } from "../lib/replayView";

function lower(label: string): string {
  return label.charAt(0).toLowerCase() + label.slice(1);
}

export function ReplayCounts({ counts, kubernetesWatched, gap = false }: { counts: Counts; kubernetesWatched: boolean; gap?: boolean }) {
  const tiles = countTiles(counts);
  const seen = tiles.filter((t) => t.value !== undefined);
  const unseen = tiles.filter((t) => t.value === undefined);
  // The node counts that were not observed, with a reason when the
  // response gives one: no Kubernetes watch, or a gap step between A and B.
  const nodes = unseen.filter((t) => t.k8s);
  const reason = !kubernetesWatched ? "unwatched" : gap ? "gap" : undefined;
  const explained = reason ? nodes : [];
  const rest = unseen.filter((t) => !explained.includes(t));
  const named = explained.map((t) => lower(t.label)).join(" and ");
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
        {explained.length > 0 && (
          <p className="rp-unseen">
            {reason === "unwatched"
              ? `Kubernetes is not watched for this system, so ${named} are not counted.`
              : `There is a gap between A and B (see the gap steps), which can leave ${named} not counted.`}
          </p>
        )}
      </div>
    </section>
  );
}
