// Text with `backticked` names (a configuration key such as accounts)
// set in mono, so a sentence of microView.ts can mark them.
import { Fragment } from "react";

export function Ticks({ text }: { text: string }) {
  const parts = text.split(/`([^`]+)`/g);
  return (
    <>
      {parts.map((p, i) =>
        i % 2 === 1 ? (
          <span key={i} className="mono">
            {p}
          </span>
        ) : (
          <Fragment key={i}>{p}</Fragment>
        ),
      )}
    </>
  );
}
