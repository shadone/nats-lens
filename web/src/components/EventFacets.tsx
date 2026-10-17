// The Events page's choices with their counts: what to show, sources and
// servers. Counts are events, from the API's facets and histogram.
import { useState } from "react";
import type { EventFeed } from "../api/events";
import type { EventSourceName } from "../api/model";
import { fmtInt } from "../lib/format";

export const SOURCES: { id: string; label: string; source: EventSourceName }[] = [
  { id: "nats", label: "NATS", source: "nats" },
  { id: "kubernetes", label: "Kubernetes", source: "kubernetes" },
  { id: "findings", label: "Findings", source: "finding" },
  { id: "collector", label: "Collector", source: "collector" },
];

/** Servers shown before "N more". */
const SERVERS_SHOWN = 8;

/**
 * Events worth reading and every event, over the histogram, which counts
 * every filter but hide_routine; without one, from the counts the page has.
 */
export function viewCounts(feed: EventFeed, worthReading: boolean): { worth?: number; all?: number } {
  if (feed.histogram.length) {
    const worth = feed.histogram.reduce((s, b) => s + b.warning + b.notable, 0);
    return { worth, all: worth + feed.histogram.reduce((s, b) => s + b.routine, 0) };
  }
  return worthReading ? { worth: feed.totalCount, all: feed.totalCount + feed.routineHidden } : { all: feed.totalCount };
}

function Count({ n }: { n: number | null }) {
  return <span className="mono n">{n === null ? "–" : fmtInt(n)}</span>;
}

function Chip({ on, onClick, label, n }: { on: boolean; onClick: () => void; label: string; n?: number | null }) {
  // n: undefined for no count, null for a count not known.
  return (
    <button type="button" className={`chip facet${on ? " on" : ""}`} aria-pressed={on} onClick={onClick}>
      {label} {n !== undefined && <Count n={n} />}
    </button>
  );
}

export function EventFacets({ feed, worthReading, setWorthReading, srcIds, toggleSource, clearSources, server, setServer, showServers }: {
  feed: EventFeed;
  worthReading: boolean;
  setWorthReading: (on: boolean) => void;
  srcIds: string[];
  toggleSource: (id: string) => void;
  clearSources: () => void;
  server: string;
  setServer: (name: string) => void;
  /** Off while a stream or consumer filters the list. */
  showServers: boolean;
}) {
  const [allServers, setAllServers] = useState(false);
  const counts = viewCounts(feed, worthReading);
  const bySource = new Map(feed.sources.map((s) => [s.source, s.count]));
  const servers = feed.servers.some((s) => s.server === server) || !server ? feed.servers : [...feed.servers, { server, count: 0 }];
  const shown = allServers ? servers : servers.filter((s, i) => i < SERVERS_SHOWN || s.server === server);
  return (
    <div className="filters ev-facets">
      <div className="grp" role="group" aria-label="Show">
        <span className="lbl">Show</span>
        <Chip on={worthReading} onClick={() => setWorthReading(true)} label="Worth reading" n={counts.worth ?? null} />
        <Chip on={!worthReading} onClick={() => setWorthReading(false)} label="Everything" n={counts.all ?? null} />
      </div>
      <div className="grp" role="group" aria-label="Sources">
        <span className="lbl">Source</span>
        <Chip on={srcIds.length === 0} onClick={clearSources} label="All" />
        {SOURCES.map((s) => (
          <Chip key={s.id} on={srcIds.includes(s.id)} onClick={() => toggleSource(s.id)} label={s.label} n={bySource.get(s.source) ?? 0} />
        ))}
      </div>
      {showServers && servers.length > 0 && (
        <div className="grp" role="group" aria-label="Servers">
          <span className="lbl">Server</span>
          {shown.map((s) => (
            <Chip key={s.server} on={s.server === server} onClick={() => setServer(s.server === server ? "" : s.server)} label={s.server} n={s.count} />
          ))}
          {servers.length > shown.length && (
            <button type="button" className="chip" onClick={() => setAllServers(true)}>
              {servers.length - shown.length} more
            </button>
          )}
        </div>
      )}
    </div>
  );
}
