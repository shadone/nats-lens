// The line under the header when the range starts before the history:
// "31 min of history: you asked for the last 24 h; the history begins at
// 14:29 UTC. Data history". It reads GetLensInfo, which the shell already
// reads, so it adds no request. Fleet reads no system, and the Data
// history page says it in its own summary, so neither shows it.
import { useLocation } from "react-router";
import { useLensInfo } from "../api/queries";
import { historyNoteText } from "../lib/historyNotes";
import { useResolvedRange, useTimeRange } from "../lib/useTimeRange";
import { serverNow } from "../lib/clock";
import { navFor } from "../nav";
import { RLink } from "./common";

export const DATA_HISTORY_PATH = "/data-history";

/** "Data history", linked with the page's system and range. */
export function HistoryLink({ children = "Data history" }: { children?: string }) {
  return <RLink to={DATA_HISTORY_PATH}>{children}</RLink>;
}

/** Whether a page shows the note: it reads a system and a range, and is not Data history. */
export function showsHistoryNote(pathname: string): boolean {
  return !navFor(pathname)?.crossSystem && pathname !== DATA_HISTORY_PATH;
}

export function HistoryNote() {
  const { pathname } = useLocation();
  const { spec } = useTimeRange();
  const { start, end } = useResolvedRange(spec);
  const info = useLensInfo().data;
  if (!info || !showsHistoryNote(pathname)) return null;
  const text = historyNoteText({ historyStart: info.historyStart, start, end, live: spec.kind === "live", now: serverNow() }, spec, info.collectIntervalMs);
  if (!text) return null;
  return (
    <p className="hn-note" role="note">
      {text} <HistoryLink />
    </p>
  );
}
