// "Between A and B": what changed, in numbers. Observed counts are tiles,
// a zero one quieter; the counts that were not observed are named in one
// line, never shown as 0.
import type { ReplayCounts as Counts } from "../api/adapt-replay";
import { fmtInt } from "../lib/format";
import { countTiles } from "../lib/replayView";

function lower(label: string): string {
  return label.charAt(0).toLowerCase() + label.slice(1);
}

export function ReplayCounts({ counts, kubernetesWatched }: { counts: Counts; kubernetesWatched: boolean }) {
  const tiles = countTiles(counts);
  const seen = tiles.filter((t) => t.value !== undefined);
  const unseen = tiles.filter((t) => t.value === undefined);
  // Without a Kubernetes watch the node counts cannot be observed; say why once.
  const unwatched = kubernetesWatched ? [] : unseen.filter((t) => t.k8s);
  const rest = unseen.filter((t) => !unwatched.includes(t));
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
        {unwatched.length > 0 && (
          <p className="rp-unseen">
            Kubernetes is not watched for this system, so {unwatched.map((t) => lower(t.label)).join(" and ")} are not counted.
          </p>
        )}
      </div>
    </section>
  );
}
