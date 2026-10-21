// The Capacity page's streams table: what each stream keeps, what bounds
// it, how much it holds, the limit it reaches next, and the decision on an
// unbounded one, with the button that accepts it as by design.
import { useEffect, useRef, useState } from "react";
import type { Capacity, CapacityStream } from "../api/capacity";
import type { Mute } from "../api/model";
import {
  ACCEPT_CHECK,
  boundText,
  dataHeld,
  declaredText,
  fillsSoonText,
  kindLine,
  mutedUntil,
  nextLimitText,
  perDayTimesR,
  sizeTimesR,
} from "../lib/capacityView";
import { fmtBytes, fmtGrowth, fmtInt } from "../lib/format";
import { muteBy, reviewOn, sentence } from "../lib/muteView";
import { accountLabel, findingPath, streamPath } from "../lib/view";
import { AcceptDialog, type AcceptTarget } from "./AcceptDialog";
import { Num, RLink } from "./common";

function rowKey(s: CapacityStream): string {
  return `${s.account}/${s.stream}`;
}

function acceptTarget(s: CapacityStream): AcceptTarget {
  return {
    findingId: s.findingId,
    checkCode: ACCEPT_CHECK.code,
    checkName: ACCEPT_CHECK.name,
    entity: { kind: "stream", key: rowKey(s), name: s.stream, account: s.account, accountName: s.accountName || undefined, stream: s.stream, consumer: "", server: "" },
  };
}

function FindingLink({ s, children, className }: { s: CapacityStream; children: React.ReactNode; className?: string }) {
  return s.findingId ? (
    <RLink to={findingPath(s.findingId)} className={`x${className ? ` ${className}` : ""}`}>
      {children}
    </RLink>
  ) : (
    <span className={className}>{children}</span>
  );
}

function AcceptedNote({ m }: { m: Mute }) {
  return (
    <small>
      {muteBy(m)}: {sentence(m.reason)} {reviewOn(m)}
    </small>
  );
}

function StatusCell({ s, onAccept }: { s: CapacityStream; onAccept: (s: CapacityStream) => void }) {
  switch (s.status) {
    case "fillsSoon":
      return (
        <>
          <FindingLink s={s} className="cap-tag cap-warn">
            {fillsSoonText(s)}
          </FindingLink>
          {s.mute && <small>{mutedUntil(s.mute.expiresAt)}</small>}
        </>
      );
    case "accepted":
      return (
        <>
          <span className="cap-design">BY DESIGN</span>
          {s.mute && <AcceptedNote m={s.mute} />}
          {s.findingId && (
            <small>
              <FindingLink s={s}>{ACCEPT_CHECK.code}</FindingLink>
            </small>
          )}
        </>
      );
    case "declared":
      return (
        <>
          <span className="cap-design">BY DESIGN</span>
          <small>{declaredText(s.declaredReason)}</small>
        </>
      );
    case "muted":
      return <FindingLink s={s}>{mutedUntil(s.mute?.expiresAt)}</FindingLink>;
    case "awaiting":
      return (
        <>
          <button type="button" className="btn cap-accept" onClick={() => onAccept(s)} aria-label={`Accept ${s.stream} as by design…`}>
            Accept as by design…
          </button>
          <small>
            <FindingLink s={s}>{ACCEPT_CHECK.code}</FindingLink> awaits a decision
          </small>
        </>
      );
    case "unbounded":
      return <span className="cap-quiet">unbounded</span>;
    case "bounded":
      return (
        <span className="cap-ok">
          <span aria-hidden="true">● </span>bounded
        </span>
      );
    default:
      return <span className="cap-quiet">–</span>;
  }
}

function HeldCell({ s, epochAt }: { s: CapacityStream; epochAt: number }) {
  const h = dataHeld(s, epochAt);
  return (
    <>
      <div className="cap-held" aria-hidden="true">
        {h.pct > 0 && <div className={`cap-held-fill${h.bounded ? "" : " cap-held-open"}`} style={{ left: `${100 - h.pct}%` }} />}
      </div>
      <div className="cap-held-label">{h.label}</div>
    </>
  );
}

/** An acceptance saved on this page, and the time of the answer shown when it was saved. */
interface Saved {
  mute: Mute;
  shownAt: number;
}

/**
 * Rows the page accepted, shown as accepted until an answer newer than the
 * one shown at the save arrives; that answer is then shown, whatever it says (the
 * acceptance may have ended elsewhere in the meantime).
 */
export function withAccepted(rows: CapacityStream[], saved: Record<string, Saved>, answeredAt: number): CapacityStream[] {
  return rows.map((s) => {
    const a = saved[s.findingId];
    return s.status === "awaiting" && a && answeredAt <= a.shownAt ? { ...s, status: "accepted", mute: a.mute } : s;
  });
}

/** answeredAt: when the answer d was fetched (the query's dataUpdatedAt). */
export function StreamsTable({ d, answeredAt }: { d: Capacity; answeredAt: number }) {
  const [accepting, setAccepting] = useState<CapacityStream>();
  const [saved, setSaved] = useState<Record<string, Saved>>({});
  // The row whose acceptance was just saved: its button is gone, so focus moves to its stream link.
  const [focusRow, setFocusRow] = useState("");
  const tableRef = useRef<HTMLElement>(null);
  useEffect(() => {
    if (!focusRow) return;
    tableRef.current?.querySelector<HTMLElement>(`tr[data-finding="${CSS.escape(focusRow)}"] .cap-name`)?.focus();
    setFocusRow("");
  }, [focusRow]);
  const shown = useRef(answeredAt);
  shown.current = answeredAt;
  const rows = withAccepted(d.streams, saved, answeredAt);
  const accounts = new Set(rows.map((s) => s.account)).size;
  const epochAt = d.epochAt ?? 0;
  return (
    <>
      <section ref={tableRef} className="card cap-card" aria-label="Streams by what bounds them">
        {rows.length === 0 ? (
          <p className="cap-foot">No stream in this collection.</p>
        ) : (
          <div className="table-wrap scroll-cue">
            <table className="tbl cap-tbl">
              <thead>
                <tr>
                  <th scope="col">Stream</th>
                  <th scope="col">Bounded by</th>
                  <th scope="col">Data held</th>
                  <th scope="col" className="num">
                    Size × R
                  </th>
                  <th scope="col" className="num">
                    /day
                  </th>
                  <th scope="col">Next limit</th>
                  <th scope="col">Status</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((s) => {
                  const next = nextLimitText(s);
                  const perDay = perDayTimesR(s);
                  return (
                    <tr key={rowKey(s)} data-finding={s.findingId || undefined}>
                      <th scope="row">
                        <RLink to={streamPath(s.account, s.stream)} className="x cap-name">
                          {s.stream}
                        </RLink>
                        <small>{kindLine(s)}</small>
                        {accounts > 1 && <small>account {accountLabel(s.account, s.accountName || undefined)}</small>}
                      </th>
                      <td>{boundText(s)}</td>
                      <td className="cap-held-cell">
                        <HeldCell s={s} epochAt={epochAt} />
                      </td>
                      <td className="num">
                        <Num v={sizeTimesR(s)} fmt={fmtBytes} />
                      </td>
                      <td className="num cap-rate">
                        <Num v={perDay} fmt={fmtGrowth} />
                      </td>
                      <td className={next.crit ? "cap-crit" : undefined}>{next.text}</td>
                      <td className="cap-status">
                        <StatusCell s={s} onAccept={setAccepting} />
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
        {d.streamsTotal > d.streams.length && (
          <p className="cap-foot">
            The first {fmtInt(d.streams.length)} of {fmtInt(d.streamsTotal)} streams are listed.
          </p>
        )}
      </section>
      <p className="cap-note">
        Data held spans from the oldest retained message to the collection on a one-year scale; Size × R and /day count every replica. Accept as by design records a reason, who
        accepted it and a review date; the finding then leaves the Overview until that date, or until nats-lens sees the stream's growth over a week change by more than half.
      </p>
      <AcceptDialog
        target={accepting ? acceptTarget(accepting) : undefined}
        onClose={() => setAccepting(undefined)}
        onAccepted={(id, m) => {
          setSaved((a) => ({ ...a, [id]: { mute: m, shownAt: shown.current } }));
          setFocusRow(id);
        }}
      />
    </>
  );
}
