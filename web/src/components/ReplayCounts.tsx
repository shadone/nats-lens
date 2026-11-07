// "Between A and B": what changed, in numbers. Observed counts are tiles,
// a zero one quieter; the counts that were not observed are named, grouped
// by the reason the response gives, never shown as 0.
import type { ReplayCounts as Counts, Unobserved } from "../api/adapt-replay";
import { fmtInt, hm } from "../lib/format";
import { countTiles, unsetGroups, type CountTile } from "../lib/replayView";

function lower(label: string): string {
  return label.charAt(0).toLowerCase() + label.slice(1);
}

/** "a, b and c" */
function listText(items: string[]): string {
  return items.length < 2 ? items.join("") : `${items.slice(0, -1).join(", ")} and ${items[items.length - 1]}`;
}

export function ReplayCounts({ counts, unobserved = [], fmt = hm }: { counts: Counts; unobserved?: Unobserved[]; fmt?: (t: number) => string }) {
  const tiles = countTiles(counts);
  const seen = tiles.filter((t) => t.value !== undefined);
  // Why each unset count is unset comes from the response, grouped by
  // reason and times; a count without a reason is only named.
  const groups = unsetGroups(tiles, unobserved, fmt);
  const names = (ts: CountTile[]) => listText(ts.map((t) => lower(t.label)));
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
        {groups.map((g) => (
          <p key={g.reason ?? ""} className="rp-unseen">
            <b>{g.reason ? `Not observed (${g.reason}):` : "Not observed:"}</b> {names(g.tiles)}.
          </p>
        ))}
      </div>
    </section>
  );
}
