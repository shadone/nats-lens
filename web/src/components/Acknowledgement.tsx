import { useEffect, useRef, useState, type ReactNode } from "react";
import type { Acknowledgement, Finding } from "../api/model";
import { useSystem } from "../lib/useSystem";
import { serverNow } from "../lib/clock";
import { isoMinute } from "../lib/format";
import { AckDialog } from "./AckDialog";
/** Quiet expiring does not end ownership; use the server flag and end time. */
export function AcknowledgementMark({ acknowledgement: a, now = serverNow() }: { acknowledgement?: Acknowledgement; now?: number }) {
  if (!a) return null;
  const ended = a.endedAt !== undefined;
  return <span className="oc-mark">
    Acknowledged by {a.ackedBy || "unknown"}{a.createdAt !== undefined && ` ${isoMinute(a.createdAt).slice(11)} UTC`}{a.note && `: ${a.note}`}
    {ended ? `. ${a.endReason === "replaced" ? "Replaced" : "Handed back"} by ${a.endedBy || "unknown"} at ${isoMinute(a.endedAt!)} UTC.` :
      a.quiet && (a.quietUntil === undefined || now < a.quietUntil) ? a.quietUntil !== undefined ? ` · quiet until ${isoMinute(a.quietUntil).slice(11)} UTC` : " · quiet until resolved" : " · alerts are no longer quiet"}
  </span>;
}
/** One action owner per finding tree. A successful save focuses the receipt
 * after the native dialog has restored its opener's focus. */
type FindingAcknowledgementProps = { finding: Finding; phone?: boolean; renderHead?: (open: () => void) => ReactNode };
export function FindingAcknowledgement(props: FindingAcknowledgementProps) {
  const system = useSystem();
  const f = props.finding;
  // Visit receipts and pending dialog callbacks belong only to this episode.
  // Resolving clears them too, even when the episode's start stays the same.
  return <EpisodeAcknowledgement key={JSON.stringify([system, f.id, f.since, f.state === "resolved"])} {...props} />;
}
function EpisodeAcknowledgement({ finding: f, phone = false, renderHead }: FindingAcknowledgementProps) {
  const [dialog, setDialog] = useState<"ack" | "release">();
  const [previous, setPrevious] = useState<Acknowledgement>();
  const [focusVersion, setFocusVersion] = useState(0);
  const section = useRef<HTMLElement>(null);
  const current = f.acknowledgement;
  const open = () => setDialog("ack");
  useEffect(() => {
    if (focusVersion) section.current?.focus({ preventScroll: false });
  }, [focusVersion]);
  const completed = (next: Acknowledgement) => {
    if (current && current.id !== next.id) setPrevious({ ...current, endedAt: next.createdAt, endedBy: next.ackedBy, endReason: "replaced", quiet: false });
    else if (next.endedAt !== undefined) setPrevious(next);
    setFocusVersion(n => n + 1);
  };
  const shown = current || previous;
  return <>
    {renderHead?.(open)}
    <section className="oc-ack sec card box-card" aria-label="Acknowledgement" tabIndex={-1} ref={section}>
      <h2 className="oc-heading">Acknowledgement</h2>
      <div aria-live="polite" aria-atomic="true">
        {shown ? <p><AcknowledgementMark acknowledgement={shown} /></p> : <p>No acknowledgement for this episode.</p>}
        {current && previous && previous.id !== current.id && <p><AcknowledgementMark acknowledgement={previous} /></p>}
      </div>
      {f.state !== "resolved" && <div className="oc-actions">
        {current ? <><button type="button" className="btn" onClick={open}>Update…</button><button type="button" className="btn" onClick={() => setDialog("release")}>Hand back…</button></> :
          !phone && !renderHead && <button type="button" className="btn" onClick={open}>Acknowledge…</button>}
      </div>}
    </section>
    {phone && f.state !== "resolved" && <div className="oc-sticky"><button type="button" className="btn p oc-primary" onClick={open}>{current ? "Update acknowledgement…" : "I'm on it"}</button></div>}
    <AckDialog finding={dialog ? { ...f, acknowledgement: current } : undefined} handBack={dialog === "release"} onClose={() => setDialog(undefined)} onSaved={completed} />
  </>;
}
