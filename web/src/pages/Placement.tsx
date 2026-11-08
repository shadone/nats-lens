import { deferredView } from "../components/deferredView";
// Placement: every stream and durable consumer as each server sees it, a
// row per asset and a column per server, with filters, a moment to compare
// with and a side panel for the selected cell. Filters, comparison and
// selection live in the URL.
import { useMemo, useRef } from "react";
import { useSearchParams } from "react-router";
import { matrixFilter } from "../api/placementMatrix";
import { usePlacement } from "../api/queries";
import type { CellRef } from "../components/PlacementMatrix";
import { compareSpec, changes as diff, compareAt as momentOf } from "../lib/placementView";
import { rangeKey } from "../lib/timeRange";
import { usePageTitle } from "../lib/usePageTitle";
import { useSystem } from "../lib/useSystem";
import { useResolvedRange, useTimeRange } from "../lib/useTimeRange";

function usePlacementModel() {
  usePageTitle("Placement");
  const [params, setParams] = useSearchParams();
  const { spec } = useTimeRange();
  const sys = useSystem();
  const { end } = useResolvedRange(spec);
  const filter = matrixFilter(params.get("filter"));
  const cmp = params.get("cmp") ?? "";
  const allServers = params.get("servers") === "all";
  const selected: CellRef | undefined = params.get("asset") && params.get("server") ? { asset: params.get("asset")!, server: params.get("server")! } : undefined;
  const q = usePlacement(spec, filter, true, undefined, selected);
  // The moment to compare with, to the minute, so a live range does not
  // ask for a new one every tick.
  const raw = momentOf(cmp, end);
  const at = raw === undefined ? undefined : Math.floor(raw / 60_000) * 60_000;
  // A newly chosen moment shows nothing of the one before while it
  // loads, nor does a change of system or range; a live range's moment
  // moving to the next minute keeps it, since a live range's key names
  // its preset, not the minute. Changes are counted over every asset,
  // not only the listed ones, so the page also reads the unfiltered
  // matrix (the same call as the matrix itself when no filter applies).
  const cq = usePlacement(compareSpec(at ?? 0), "all", at !== undefined, `${sys}|${rangeKey(spec)}|${cmp}`);
  const all = usePlacement(spec, "all", at !== undefined);
  // The current side counts only once it is this system's and range's
  // own matrix, not the previous one kept while it loads. The matrix on
  // screen must be too: with a filter it is a call of its own, and the
  // one kept while it loads may be another system's or range's. One
  // kept across a change of the filter alone is this system's and
  // range's, so the marks stay on it. An error on either side, or of
  // the filtered matrix while none has loaded for this system and
  // range, marks nothing, though the data before it stays.
  const here = `${sys}|${rangeKey(spec)}`;
  const shownFor = useRef<string | undefined>(undefined);
  if (q.data !== undefined && !q.isPlaceholderData) shownFor.current = here;
  const failed = cq.error ?? all.error ?? (shownFor.current !== here ? q.error : null);
  const current = all.isPlaceholderData || shownFor.current !== here ? undefined : all.data;
  const changed = useMemo(() => (current && cq.data && at !== undefined && !failed ? diff(current, cq.data) : undefined), [current, cq.data, at, failed]);
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
  return { end, filter, cmp, allServers, selected, q: { ...q }, at, cq: { ...cq }, failed, current, changed, panel, set, select, close };
}
export type PlacementModel = ReturnType<typeof usePlacementModel>;
const View = deferredView(() => import("../views/Placement").then(m => m.PlacementView));
export function Placement() {
  const model = usePlacementModel();
  return <View model={model} />;
}
