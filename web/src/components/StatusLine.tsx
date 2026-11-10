// The Overview's status line: one short line and the unmuted findings per
// severity, as status text rather than a heading, since the header holds the
// page's heading. The cards below say what the findings are.
import type { Freshness, Tally } from "../api/model";
import { quietCount, statusLine } from "../lib/statusLine";
import { LEVEL_LABEL } from "../lib/view";
import { HealthIcon, Sev } from "./Severity";

export function StatusLine({ tally, freshness, live, now }: { tally: Tally; freshness?: Freshness; live: boolean; now: number }) {
  const s = statusLine(tally, freshness, live, now);
  const quiet = quietCount(tally);
  return (
    <div className="status-line" role="status" aria-label="Status">
      <p className="status-text">
        <span aria-hidden="true">
          <HealthIcon level={s.level} />
        </span>
        {s.text}
      </p>
      {!!tally.acknowledged && <span className="oc-count">{tally.acknowledged} acknowledged</span>}
      {quiet && <span className="oc-count">{quiet}</span>}
      {s.badges.map((b) => (
        <Sev key={b.level} level={b.level} label={`${b.count} ${LEVEL_LABEL[b.level].toUpperCase()}`} />
      ))}
    </div>
  );
}
