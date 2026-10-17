// Pieces shared by the list and detail pages: pager, stat cards, consumer
// table, disagreements, findings and events for one entity.
import { useState, type ReactNode } from "react";
import type { ConsumerSummary, Disagreement, Entity } from "../api/model";
import { useEvents, useFindings } from "../api/queries";
import { fmtDuration, fmtInt } from "../lib/format";
import { consumerPath } from "../lib/view";
import { useResolvedRange, useTimeRange } from "../lib/useTimeRange";
import { Dash, Empty, Num, QueryView, RLink, Section } from "./common";
import { EventRow } from "./EventRow";
import { FindingRow } from "./FindingParts";
import { HealthIcon } from "./Severity";

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
  return (
    <nav className="pager" aria-label="Pagination">
      <span>
        {fmtInt(shown)} shown of {fmtInt(total)} · page {page}
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

export function Stat({ label, value, sub, bad }: { label: string; value: ReactNode; sub?: ReactNode; bad?: boolean }) {
  return (
    <div className="card stat">
      <span className="lbl">{label}</span>
      <span className={`v${bad ? " bad" : ""}`}>{value}</span>
      {sub && <small>{sub}</small>}
    </div>
  );
}

export function ConsumersTable({ consumers, end, showStream = true }: { consumers: ConsumerSummary[]; end: number; showStream?: boolean }) {
  return (
    <div className="card table-wrap">
      <table className="tbl">
        <thead>
          <tr>
            <th scope="col">
              <span className="sr-only">Status</span>
            </th>
            <th scope="col">Consumer</th>
            <th scope="col" className="num">
              Pending
            </th>
            <th scope="col" className="num">
              Ack pending
            </th>
            <th scope="col" className="num">
              Redelivered
            </th>
            <th scope="col" className="num">
              Waiting
            </th>
            <th scope="col">Leader</th>
            <th scope="col" className="num">
              Last delivery
            </th>
            <th scope="col">Notes</th>
          </tr>
        </thead>
        <tbody>
          {consumers.map((c) => {
            const e = c.entity;
            const notes = [
              !c.present && "missing",
              c.paused && "paused",
              c.serversDisagree && "servers disagree",
              !c.durable && "ephemeral",
              ...c.findingCodes,
            ].filter(Boolean);
            return (
              <tr key={e.key}>
                <td>
                  <HealthIcon level={c.status} />
                </td>
                <td className="mono">
                  <RLink to={consumerPath(e.account, e.stream, e.consumer)}>{e.consumer || e.name}</RLink>
                  {showStream && <small>{e.stream}</small>}
                </td>
                <td className="num"><Num v={c.numPending} /></td>
                <td className="num"><Num v={c.numAckPending} /></td>
                <td className="num"><Num v={c.numRedelivered} /></td>
                <td className="num"><Num v={c.numWaiting} /></td>
                <td className="mono">{c.leader || <Dash />}</td>
                <td className="num">{c.lastDelivered !== undefined ? `${fmtDuration(end - c.lastDelivered)} ago` : <Dash />}</td>
                <td className={`notes${!c.present ? " bad" : ""}`}>{notes.join(", ")}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

export function Disagreements({ items }: { items: Disagreement[] }) {
  if (!items.length) return null;
  const servers = [...new Set(items.flatMap((d) => d.values.map((v) => v.server)))];
  return (
    <Section title="Servers disagree" aside="Each server's own view of the settings that differ.">
      <div className="card table-wrap disagree">
        <table className="tbl">
          <thead>
            <tr>
              <th scope="col">Setting</th>
              {servers.map((s) => (
                <th scope="col" key={s}>
                  {s}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {items.map((d) => (
              <tr key={d.field}>
                <td className="mono">{d.field}</td>
                {servers.map((s) => (
                  <td key={s} className="mono">
                    {d.values.find((v) => v.server === s)?.value ?? <Dash />}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Section>
  );
}

/** Current findings for an entity. */
export function EntityFindings({ entity }: { entity: Entity }) {
  const { spec } = useTimeRange();
  const { start, end } = useResolvedRange(spec);
  const q = useFindings(spec, { entity, includeMuted: true });
  return (
    <Section title="Findings" aside="Firing now for this entity, worst first.">
      <QueryView q={q} what="findings">
        {(p) =>
          p.items.length ? (
            <div className="card others">
              {p.items.map((f) => (
                <FindingRow key={f.id} f={f} end={end} rangeStart={start} />
              ))}
            </div>
          ) : (
            <Empty>No finding fires for this entity.</Empty>
          )
        }
      </QueryView>
    </Section>
  );
}

/** The newest events that concern an entity, with a link to all of them. */
export function EntityEvents({ entity, query }: { entity: Entity; query: string }) {
  const { spec } = useTimeRange();
  const { end } = useResolvedRange(spec);
  const q = useEvents(spec, { entity, pageSize: 15 });
  const [open, setOpen] = useState<string>();
  return (
    <Section title="Events" aside={<RLink to={`/events?${query}`}>All events for this entity</RLink>}>
      <QueryView q={q} what="events">
        {(p) =>
          p.items.length ? (
            <div className="card evlist">
              {p.items.map((e) => (
                <EventRow key={e.id} e={e} end={end} open={open === e.id} onToggle={() => setOpen(open === e.id ? undefined : e.id)} />
              ))}
            </div>
          ) : (
            <Empty>No events for this entity in the range.</Empty>
          )
        }
      </QueryView>
    </Section>
  );
}
