import { useState } from "react";
import { fmtInt } from "../lib/format";

/**
 * Paging over page tokens: a stack of the tokens seen. A token is valid only
 * with the system and filters it came from, so the key holds them all, and
 * a new key starts again at page one within the same render.
 */
export function usePager(key: string) {
  const [st, setSt] = useState({ key, stack: [""] });
  // A new key drops the old stack for good, so returning to earlier
  // filters starts at page one too.
  if (st.key !== key) setSt({ key, stack: [""] });
  const stack = st.key === key ? st.stack : [""];
  return {
    token: stack[stack.length - 1],
    page: stack.length,
    next: (t: string) => setSt({ key, stack: [...stack, t] }),
    prev: () => setSt({ key, stack: stack.length > 1 ? stack.slice(0, -1) : stack }),
  };
}

export function Pager({ page, next, onNext, onPrev, total, shown, newer = "Previous", older = "Next" }: {
  page: number;
  next: string;
  onNext: () => void;
  onPrev: () => void;
  total: number;
  shown: number;
  newer?: string;
  older?: string;
}) {
  const count = `${fmtInt(shown)} shown of ${fmtInt(total)}`;
  if (page <= 1 && !next) return <p className="pager">{count}</p>;
  return (
    <nav className="pager" aria-label="Pagination">
      <span>
        {count} · page {page}
      </span>
      <span className="btns">
        <button type="button" className="btn" onClick={onPrev} disabled={page <= 1}>
          {newer}
        </button>
        <button type="button" className="btn" onClick={onNext} disabled={!next}>
          {older}
        </button>
      </span>
    </nav>
  );
}
