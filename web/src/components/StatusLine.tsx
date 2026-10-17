// The Overview's status line: one short line and the unmuted findings per
// severity. The cards below say what the findings are.
import type { Freshness, Tally } from "../api/model";
import { statusLine } from "../lib/statusLine";
import { LEVEL_LABEL } from "../lib/view";
import { HealthIcon, Sev } from "./Severity";

export function StatusLine({ tally, freshness, live, now }: { tally: Tally; freshness?: Freshness; live: boolean; now: number }) {
  const s = statusLine(tally, freshness, live, now);
  return (
    <section className="status-line" aria-label="Status">
      <h2>
        <span aria-hidden="true">
          <HealthIcon level={s.level} />
        </span>
        {s.text}
      </h2>
      {s.badges.map((b) => (
        <Sev key={b.level} level={b.level} label={`${b.count} ${LEVEL_LABEL[b.level].toUpperCase()}`} />
      ))}
    </section>
  );
}
