// Data history: what nats-lens holds of a system: since when, how long it
// keeps it, where the range has gaps, what uses the history, and what the
// archive holds.
import { useDataHistory, useShellStatus } from "../api/queries";
import { QueryView, RLink } from "../components/common";
import { ArchiveCard, CoverageBoxes, GapsCard, UsesCard } from "../components/HistoryParts";
import { findingsAtStartText, HISTORY_SUBTITLE, summaryParts } from "../lib/historyView";
import { usePageTitle } from "../lib/usePageTitle";
import { useNow, useTimeRange } from "../lib/useTimeRange";

export function DataHistory() {
  usePageTitle("Data history");
  const { spec } = useTimeRange();
  const live = spec.kind === "live";
  const q = useDataHistory(spec);
  const shell = useShellStatus();
  const now = useNow(30_000);
  const f = shell.data?.freshness;
  const stopped = f && !f.collecting ? f.notCollectingSince : undefined;
  const d = q.data;
  return (
    <>
      <div className="page-h">
        <span className="aside">{HISTORY_SUBTITLE}</span>
      </div>
      {d && (
        <p className="dh-summary">
          {summaryParts(d, now, stopped).map((p, i) => (
            <span key={p} className={p.startsWith("not collecting") ? "dh-warn" : undefined}>
              {i > 0 && <span className="dh-dot"> · </span>}
              {p}
            </span>
          ))}
        </p>
      )}
      <QueryView q={q} what="the data history">
        {(r) => (
          <>
            <section className="sec dh-sec" aria-label="Coverage of the range">
              <div className="sec-h">
                <h2>Coverage of the range</h2>
              </div>
              <CoverageBoxes d={r} />
              {r.historyStart !== undefined && (
                <p className="dh-line">
                  {findingsAtStartText(r.findingsAtStart, r.historyStart, now)}
                  {r.findingsAtStart > 0 && (
                    <>
                      {" "}
                      <RLink to="/findings">Findings</RLink>
                    </>
                  )}
                </p>
              )}
            </section>
            <GapsCard d={r} live={live} />
            <UsesCard d={r} now={now} />
            <ArchiveCard d={r} now={now} />
          </>
        )}
      </QueryView>
    </>
  );
}
