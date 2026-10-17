// Client-side table sorting with the column and direction in the URL
// (?sort=bytes, ?sort=-bytes for descending), so a sorted view can be
// shared. Values that were not observed always sort last.
import { useCallback } from "react";
import { useSearchParams } from "react-router";

export type SortDir = "asc" | "desc";

export interface SortState {
  key: string;
  dir: SortDir;
}

export const SORT_PARAM = "sort";

export function readSort(v: string | null): SortState | undefined {
  if (!v) return undefined;
  return v.startsWith("-") ? { key: v.slice(1), dir: "desc" } : { key: v, dir: "asc" };
}

/**
 * The page's sort and a function that sorts by a column: a new column
 * starts in its first direction, the same column flips.
 */
export function useSort(): [SortState | undefined, (key: string, first: SortDir) => void] {
  const [params, setParams] = useSearchParams();
  const sort = readSort(params.get(SORT_PARAM));
  const by = useCallback(
    (key: string, first: SortDir) =>
      setParams(
        (p) => {
          const cur = readSort(p.get(SORT_PARAM));
          const dir: SortDir = cur?.key === key ? (cur.dir === "asc" ? "desc" : "asc") : first;
          const n = new URLSearchParams(p);
          n.set(SORT_PARAM, dir === "desc" ? `-${key}` : key);
          return n;
        },
        { replace: true },
      ),
    [setParams],
  );
  return [sort, by];
}

export type SortValue = number | string | undefined;

/** rows sorted by value in dir, unset values last, ties in their given order. */
export function sortRows<T>(rows: T[], value: (r: T) => SortValue, dir: SortDir): T[] {
  const sign = dir === "asc" ? 1 : -1;
  return rows
    .map((r, i) => ({ r, i, v: value(r) }))
    .sort((a, b) => {
      if (a.v === undefined || b.v === undefined) return (a.v === undefined ? 1 : 0) - (b.v === undefined ? 1 : 0) || a.i - b.i;
      const c = typeof a.v === "string" || typeof b.v === "string" ? String(a.v).localeCompare(String(b.v)) : a.v - b.v;
      return c * sign || a.i - b.i;
    })
    .map((x) => x.r);
}
