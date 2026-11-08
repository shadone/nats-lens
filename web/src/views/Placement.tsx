import type { PlacementModel } from "../pages/Placement";
// Placement: every stream and durable consumer as each server sees it, a
// row per asset and a column per server, with filters, a moment to compare
// with and a side panel for the selected cell. Filters, comparison and
// selection live in the URL.
import { useEffect } from "react";
import { MATRIX_FILTERS, assetsOf, type MatrixFilter } from "../api/placementMatrix";
import { Empty, QueryView, errorText } from "../components/common";
import { PlacementMatrix } from "../components/PlacementMatrix";
import { PlacementPanel } from "../components/PlacementPanel";
import { hms } from "../lib/format";
import { momentText } from "../lib/placementView";

/** "2026-10-16T14:30" for a datetime-local input, in UTC. */
function inputValue(at: number | undefined): string {
  return at === undefined ? "" : new Date(at).toISOString().slice(0, 16);
}

export function PlacementView({ model }: { model: PlacementModel }) {
  const { end, filter, cmp, allServers, selected, q, at, cq, failed, current, changed, panel, set, select, close } = model;

  // A new selection brings the cell and the panel into view; the panel
  // sits below the matrix at phone width.
  useEffect(() => {
    if (!selected) return;
    // The selected cell may sit past the servers in view.
    document.querySelector(".pm-cell.on")?.scrollIntoView?.({ block: "nearest", inline: "nearest" });
    panel.current?.querySelector("h2")?.scrollIntoView?.({ block: "nearest" });
  }, [selected?.asset, selected?.server, q.data === undefined]);

  return (
    <>
      <div className="page-h">
        <span className="aside">
          Every stream and durable consumer, as each server sees it
          {q.data?.epochAt !== undefined ? (
            <>
              , in the collection of <span className="mono">{hms(q.data.epochAt)} UTC</span>
            </>
          ) : (
            ", in the newest collection of the range"
          )}
          .
        </span>
      </div>
      <div className="filters pm-filters">
        <div className="grp" role="group" aria-label="Filter">
          {MATRIX_FILTERS.map((f) => (
            <FilterChip key={f.id} id={f.id} label={f.label} n={q.data?.counts[f.id]} on={filter === f.id} onClick={() => set({ filter: f.id === "all" ? "" : f.id })} />
          ))}
        </div>
        <div className="grp pm-cmp">
          <label className="grp">
            <span className="lbl">Compare with</span>
            <select value={cmp === "" || cmp === "1d" ? cmp : "custom"} onChange={(e) => set({ cmp: e.target.value === "custom" ? inputValue(end - 86_400_000) + ":00Z" : e.target.value })}>
              <option value="">nothing</option>
              <option value="1d">this time yesterday</option>
              <option value="custom">a time of my choice</option>
            </select>
          </label>
          {cmp !== "" && cmp !== "1d" && (
            <label className="grp">
              <input type="datetime-local" aria-label="Time to compare with, UTC" value={inputValue(at)} max={inputValue(end)} onChange={(e) => e.target.value && set({ cmp: `${e.target.value}:00Z` })} />
              <span className="lbl">UTC</span>
            </label>
          )}
        </div>
      </div>
      {at !== undefined && (
        <p className="pm-cmp-line" role="status">
          {failed
            ? `Could not load the matrix to compare with: ${errorText(failed)}`
            : !cq.data || !current
              ? `Loading the matrix of ${momentText(at)}…`
              : cq.data.epochAt === undefined
                ? `No collection in the hour before ${momentText(at)}; nothing to compare with.`
                : `Compared with the collection of ${momentText(cq.data.epochAt)}: ${changed?.size ?? 0} ${changed?.size === 1 ? "cell differs" : "cells differ"}.`}
        </p>
      )}
      <QueryView q={q} what="the placement">
        {(m) => {
          if (m.epochAt === undefined) return <Empty>No collection in this range.</Empty>;
          if (m.counts.all === 0) return <Empty>No streams in the newest collection of this range.</Empty>;
          if (m.streams.length === 0) {
            return <Empty>No stream or consumer matches “{MATRIX_FILTERS.find((f) => f.id === filter)!.label}” in this collection.</Empty>;
          }
          const a = selected && assetsOf(m).find((x) => x.entity.key === selected.asset);
          const c = a?.cells.find((x) => x.server === selected?.server);
          const cmpAt = cq.data?.epochAt;
          // The servers that hold a listed replica, or held one at the
          // moment compared with.
          const listed = assetsOf(m);
          const holding = new Set(listed.flatMap((x) => x.cells.map((cl) => cl.server)));
          if (cmpAt !== undefined && changed) {
            const keys = new Set(listed.map((x) => x.entity.key));
            for (const k of changed.keys()) {
              const i = k.lastIndexOf("|");
              if (keys.has(k.slice(0, i))) holding.add(k.slice(i + 1));
            }
          }
          const shown = allServers ? m.servers : m.servers.filter((sv) => holding.has(sv.name));
          const hidden = m.servers.length - shown.length;
          return (
            <div className="pm-layout">
              <div className="pm-main">
                {(hidden > 0 || allServers) && (
                  <p className="pm-cols">
                    {allServers ? `Every server, ${m.servers.length}. ` : `The ${shown.length} servers that hold these replicas; ${hidden} hold none. `}
                    <button type="button" className="chip" onClick={() => set({ servers: allServers ? "" : "all" })}>
                      {allServers ? "Only servers with replicas" : `Show all ${m.servers.length} servers`}
                    </button>
                  </p>
                )}
                <PlacementMatrix m={{ ...m, servers: shown }} selected={selected} onSelect={select} changes={cmpAt !== undefined ? changed : undefined} compareAt={cmpAt} />
              </div>
              {a && c && (
                <div ref={panel} className="pm-side">
                  <PlacementPanel
                    a={a}
                    c={c}
                    now={m.epochAt ?? end}
                    observation={!q.isPlaceholderData && !q.error ? m.observation : undefined}
                    change={changed?.get(`${a.entity.key}|${c.server}`)}
                    compareAt={cmpAt}
                    onClose={close}
                  />
                </div>
              )}
            </div>
          );
        }}
      </QueryView>
    </>
  );
}

function FilterChip({ id, label, n, on, onClick }: { id: MatrixFilter; label: string; n?: number; on: boolean; onClick: () => void }) {
  return (
    <button
      type="button"
      className={`chip facet${on ? " on" : ""}`}
      aria-pressed={on}
      aria-label={n === undefined ? label : `${label} ${n}`}
      onClick={onClick}
      data-filter={id}
    >
      {label}
      {n !== undefined && <span className="n">{n}</span>}
    </button>
  );
}
