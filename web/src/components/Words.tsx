// A phrase of lib/microView.ts: its text as it is, its configuration keys
// (such as accounts) in mono. Text is never searched for marks.
import { Fragment } from "react";
import type { Phrase } from "../lib/microView";

export function Words({ p }: { p: Phrase }) {
  return (
    <>
      {p.map((x, i) =>
        typeof x === "string" ? (
          <Fragment key={i}>{x}</Fragment>
        ) : (
          <span key={i} className="mono">
            {x.key}
          </span>
        ),
      )}
    </>
  );
}
