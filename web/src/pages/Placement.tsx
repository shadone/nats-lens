// Placement: every stream and durable consumer as each server sees it, a
// row per asset and a column per server, with filters, a moment to compare
// with and a side panel for the selected cell. Filters, comparison and
// selection live in the URL.
import { useEffect, useMemo, useRef } from "react";
import { useSearchParams } from "react-router";
import { MATRIX_FILTERS, assetsOf, matrixFilter, type MatrixFilter } from "../api/placementMatrix";
import { usePlacement } from "../api/queries";
import { Empty, QueryView, errorText } from "../components/common";
import { PlacementMatrix, type CellRef } from "../components/PlacementMatrix";
import { PlacementPanel } from "../components/PlacementPanel";
import { hms } from "../lib/format";
import { changes as diff, compareAt as momentOf, compareSpec, momentText } from "../lib/placementView";
import { usePageTitle } from "../lib/usePageTitle";
import { useResolvedRange, useTimeRange } from "../lib/useTimeRange";

/** "2026-10-16T14:30" for a datetime-local input, in UTC. */
function inputValue(at: number | undefined): string {
  return at === undefined ? "" : new Date(at).toISOString().slice(0, 16);
}

export function Placement() {
  usePageTitle("Placement");
  const [params, setParams] = useSearchParams();
  const { spec } = useTimeRange();
  const { end } = useResolvedRange(spec);
  const filter = matrixFilter(params.get("filter"));
  const cmp = params.get("cmp") ?? "";
  const allServers = params.get("servers") === "all";
  const selected: CellRef | undefined = params.get("asset") && params.get("server") ? { asset: params.get("asset")!, server: params.get("server")! } : undefined;
  const q = usePlacement(spec, filter);
  // The moment to compare with, to the minute, so a live range does not
  // ask for a new one every tick.
  const raw = momentOf(cmp, end);
  const at = raw === undefined ? undefined : Math.floor(raw / 60_000) * 60_000;
  // The moment's matrix shows nothing of the moment before while it
  // loads. Changes are counted over every asset, not only the listed ones,
  // so the page also reads the unfiltered matrix (the same call as the
  // matrix itself when no filter applies).
  const cq = usePlacement(compareSpec(at ?? 0), "all", at !== undefined, false);
  const all = usePlacement(spec, "all", at !== undefined);
  const changed = useMemo(() => (all.data && cq.data && at !== undefined ? diff(all.data, cq.data) : undefined), [all.data, cq.data, at]);
  const panel = useRef<HTMLDivElement>(null);

  const set = (next: Record<string, string>, push = false) =>
    setParams(
      (p) => {
        const n = new URLSearchParams(p);
        for (const [k, v] of Object.entries(next)) {
          if (v) n.set(k, v);
          else n.delete(k);
        }
        return n;
      },
      { replace: !push },
    );
  const select = (r: CellRef) => set({ asset: r.asset, server: r.server }, true);
  const close = () => set({ asset: "", server: "" });

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
          {cq.error
            ? `Could not load the matrix to compare with: ${errorText(cq.error)}`
            : !cq.data || !all.data
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
