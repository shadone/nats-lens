import { Link } from "react-router";
import type { Finding, Transition, Window } from "../api/model";
import { withGlobal } from "../lib/globalQuery";
import { findingReplayEntry, windowReplayEntry } from "../lib/replayEntry";
import { momentText } from "../lib/replayUrl";
import { useGlobalQuery } from "../lib/useGlobalQuery";

/** Keep the shared system/account policy; the destination's fixed range wins. */
export function ReplayContextLink({ to, label }: { to: string; label?: string }) {
  const [path, query] = withGlobal(to, useGlobalQuery()).split("?");
  const params = new URLSearchParams(query);
  params.delete("range");
  return <Link className="x" to={`${path}?${params}`} aria-label={label}>Replay this window</Link>;
}

export function ReplayEntry({ finding, transitions, start, end, now, minGapMs }: {
  finding: Finding; transitions: Transition[]; start: number; end: number; now: number; minGapMs: number;
}) {
  const entry = findingReplayEntry(finding, transitions, start, end, now, minGapMs);
  return (
    <div className="replay-entry">
      {entry ? (
        <>
          <ReplayContextLink to={entry.to} />
          <p>{entry.observation} {entry.text} {entry.notes.join(" ")}</p>
        </>
      ) : <p>No finding transition time is available in this range. History may be missing or outside the latest 100 transitions.</p>}
    </div>
  );
}

export function WindowReplayEntries({ windows, now, minGapMs }: { windows: Window[]; now: number; minGapMs: number }) {
  if (!windows.length) return null;
  return (
    <ul className="replay-entry" aria-label="Replay observed windows">
      {windows.map((w, i) => {
        const entry = windowReplayEntry(w, now, minGapMs);
        return (
          <li key={`${w.start}:${i}`}>
            <span>{w.label}{entry && `: ${momentText(w.start)} to ${momentText(w.end)}`}. </span>
            {entry ? (
              <>
                <ReplayContextLink to={entry.to} label={`Replay this window: ${w.label}, ${momentText(w.start)}`} />
                <p>{entry.text} Includes up to five minutes on either side of the observed window. {entry.notes.join(" ")}</p>
              </>
            ) : <p>Replay unavailable: the observed window has no valid timing.</p>}
          </li>
        );
      })}
    </ul>
  );
}
