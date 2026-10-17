// A column header that sorts its table: a button inside the header cell,
// with aria-sort on the cell while the table is sorted by it.
import type { ReactNode } from "react";
import type { SortDir, SortState } from "../lib/sort";

export function SortTh({
  id,
  label,
  sort,
  onSort,
  first = "desc",
  num = false,
  title,
  marker,
  srOnly = false,
}: {
  id: string;
  label: string;
  sort: SortState | undefined;
  onSort: (key: string, first: SortDir) => void;
  /** The direction a first click sorts in: descending for numbers. */
  first?: SortDir;
  num?: boolean;
  title?: string;
  /** A footnote marker after the label, hidden from screen readers (the title says it). */
  marker?: ReactNode;
  /** The label is for screen readers only, as for a column of icons. */
  srOnly?: boolean;
}) {
  const on = sort?.key === id;
  return (
    <th scope="col" className={num ? "num" : undefined} aria-sort={on ? (sort.dir === "asc" ? "ascending" : "descending") : undefined} title={title}>
      <button type="button" className={`sort-btn${on ? " on" : ""}`} onClick={() => onSort(id, first)}>
        {srOnly ? <span className="sr-only">{label}</span> : label}
        {marker && <span aria-hidden="true">{marker}</span>}
        <svg className="sort-ic" viewBox="0 0 8 10" aria-hidden="true">
          <path d="M4 1 1 4h6Z" className={on && sort.dir === "asc" ? "act" : undefined} />
          <path d="M4 9 1 6h6Z" className={on && sort.dir === "desc" ? "act" : undefined} />
        </svg>
      </button>
    </th>
  );
}
