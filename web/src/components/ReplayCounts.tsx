// "Between A and B": what changed, in numbers. An unset count was not
// observed and shows a dash with "not observed", never 0.
import type { ReplayCounts as Counts } from "../api/adapt-replay";
import { fmtInt } from "../lib/format";
import { countTiles } from "../lib/replayView";

export function ReplayCounts({ counts }: { counts: Counts }) {
  return (
    <section className="sec rp-counts">
      <div className="sec-h">
        <h2>Between A and B</h2>
      </div>
      <ul className="card rp-tiles" aria-label="Between A and B">
        {countTiles(counts).map((t) => (
          <li key={t.label} className={`rp-tile${t.value === undefined ? " unset" : ""}`}>
            <span className="rp-tl">
              {t.label}
              {t.value === undefined ? <small>not observed</small> : t.note && <small>{t.note}</small>}
            </span>
            {t.value === undefined ? (
              <span className="rp-tv" aria-hidden="true">
                —
              </span>
            ) : (
              <span className="rp-tv">{fmtInt(t.value)}</span>
            )}
          </li>
        ))}
      </ul>
    </section>
  );
}
