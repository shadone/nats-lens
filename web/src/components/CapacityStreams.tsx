import {MetricHelp} from "./MetricHelp";
// The Capacity page's streams table: what each stream keeps, what bounds
// it, how much it holds, the limit it reaches next, and the decision on an
// unbounded one, with the button that accepts it as by design.
import { Fragment, useEffect, useRef, useState } from "react";
import { useSearchParams } from "react-router";
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
import { streamLabel, accountLabel, findingPath, streamPath } from "../lib/view";
import { useCapacityPage } from "../lib/useCapacityPage";
import { CapacityHistoryChart, resourceHistoryQuery, useCapacityHistory } from "./CapacityHistory";
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
          <span className="cap-design">By design</span>
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
          <span className="cap-design">By design</span>
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
 * one shown at the save arrives; that answer is then shown, whatever it says
 * (the acceptance may have ended elsewhere in the meantime). On a fixed range
 * the answers apply the mutes in force at the range's end, before the
 * acceptance, so there the saved acceptance stays for the table's life.
 */
export function withAccepted(rows: CapacityStream[], saved: Record<string, Saved>, answeredAt: number, live: boolean): CapacityStream[] {
  return rows.map((s) => {
    const a = saved[s.findingId];
    return s.status === "awaiting" && a && (!live || answeredAt <= a.shownAt) ? { ...s, status: "accepted", mute: a.mute } : s;
  });
}

/** answeredAt: when the answer d was fetched (the query's dataUpdatedAt); live: whether the page's range follows now. */
export function StreamsTable({ d, answeredAt, live }: { d: Capacity; answeredAt: number; live: boolean }) {
  const [params, setParams] = useSearchParams();
  // ASVS 1.2.2: allowlisted filter values; URLSearchParams encodes query changes.
  const value = params.get("resource");
  const resource = value === "stream" || value === "kv" || value === "object" ? value : "all";
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
  const loaded = withAccepted(d.streams, saved, answeredAt, live);
  const filtered = resource === "all" ? loaded : loaded.filter((s) => s.kind === resource);
  const [page, setHistoryPage] = useCapacityPage(filtered.length, 16, resource);
  const offset = page*16;
  const rows = filtered.slice(offset, offset+16);
  const [expanded, setExpanded] = useState("");
  const queries = rows.flatMap(s => [resourceHistoryQuery(s), ...(expanded === rowKey(s) ? [resourceHistoryQuery(s,true)] : [])]);
  const history = useCapacityHistory(queries);
  const accounts = new Set(loaded.map((s) => s.account)).size;
  const epochAt = d.epochAt ?? 0;
  return (
    <>
      <section ref={tableRef} className="card cap-card" aria-label="Resources by what bounds them">
        <div className="cap-resource-filter">
          <label>Resource type <select value={resource} onChange={(e) => setParams((p) => {
            const next = new URLSearchParams(p);
            if (e.target.value === "all") next.delete("resource");
            else next.set("resource", e.target.value);
            return next;
          })}>
            <option value="all">All</option>
            <option value="stream">Streams</option>
            <option value="kv">KV buckets</option>
            <option value="object">Object stores</option>
          </select></label>
          <p>{fmtInt(filtered.length)} of {fmtInt(loaded.length)} loaded resources shown. Server cards and page totals include all resource types.</p>
        </div>
        {rows.length === 0 ? (
          <p className="cap-foot">{loaded.length === 0 ? "No resources in this collection." : `No ${resource === "kv" ? "KV buckets" : resource === "object" ? "object stores" : "streams"} among the loaded resources.`}</p>
        ) : (
          <div className="table-wrap scroll-cue">
            <table className="tbl cap-tbl">
              <thead>
                <tr>
                  <th scope="col" aria-label="Resource">Resource <MetricHelp label="Resource storage history" ids={["stream.bytes"]}/></th>
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
                    <Fragment key={rowKey(s)}><tr data-finding={s.findingId || undefined}>
                      <th scope="row">
                        <RLink to={streamPath(s.account, s.stream)} className="x cap-name">
                          {streamLabel(s.stream)}
                        </RLink>
                        <span className="cap-type">{s.kind === "kv" ? "KV bucket" : s.kind === "object" ? "Object store" : "Stream"}</span>
                        {s.kind !== "stream" && <small>Underlying stream: {s.stream}</small>}
                        <small>{kindLine(s)}</small>
                        {accounts > 1 && <small>account {accountLabel(s.account, s.accountName || undefined)}</small>}
                        <CapacityHistoryChart q={history(resourceHistoryQuery(s))} title={`${streamLabel(s.stream)} logical resource bytes`} compact />
                        <button className="btn" type="button" aria-expanded={expanded === rowKey(s)} aria-label={`${expanded === rowKey(s) ? "Collapse" : "Expand"} history for ${streamLabel(s.stream)}`} onClick={() => setExpanded(expanded === rowKey(s) ? "" : rowKey(s))}>{expanded === rowKey(s) ? "Collapse history" : "Expand history"}</button>
                      </th>
                      <td>{boundText(s)}</td>
                      <td className="cap-held-cell">
                        <HeldCell s={s} epochAt={epochAt} />
                      </td>
                      <td className="num">
                        <Num v={sizeTimesR(s)} fmt={fmtBytes} />
                      </td>
                      <td className="num cap-rate">
                        {perDay === undefined ? <span title="This resource lacks usable observations spanning 6 h"><Num v={perDay} fmt={fmtGrowth} /></span> : <Num v={perDay} fmt={fmtGrowth} />}
                      </td>
                      <td className={next.crit ? "cap-crit" : undefined}>{next.text}</td>
                      <td className="cap-status">
                        <StatusCell s={s} onAccept={setAccepting} />
                      </td>
                    </tr>
                    {expanded === rowKey(s) && <tr><td colSpan={7}><div className="cap-history-expanded">
                      <CapacityHistoryChart q={history(resourceHistoryQuery(s))} title="Logical resource bytes" />
                      <CapacityHistoryChart q={history(resourceHistoryQuery(s,true))} title="Observed replica bytes" />
                      <p className="cap-note">Logical bytes use the leader report, or the largest report if no leader answers. Observed replica bytes sum only the replicas that reported; missing replicas are not estimated. These resource measurements are not physical file-store usage. Storage: {s.storage || "unknown"}.</p>
                    </div></td></tr>}
                    </Fragment>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
        {filtered.length > 16 && <div className="cap-history-pages"><span>History rows {offset+1}–{Math.min(offset+16,filtered.length)} of {filtered.length} filtered loaded resources.</span><button className="btn" aria-label="Previous resource history page" disabled={offset===0} onClick={() => setHistoryPage(page-1)}>Previous</button><button className="btn" aria-label="Next resource history page" disabled={offset+16>=filtered.length} onClick={() => setHistoryPage(page+1)}>Next</button></div>}
        {d.streamsTotal > d.streams.length && (
          <p className="cap-foot">
            The first {fmtInt(d.streams.length)} of {fmtInt(d.streamsTotal)} resources are loaded. Type filters apply only to these loaded resources.
          </p>
        )}
      </section>
      <p className="cap-note">
        Data held spans from the oldest retained message to the collection on a one-year scale; Size × R and /day multiply the logical leader value by configured replicas; they are estimates, not observed physical usage. Accept as by design records a reason, who
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
