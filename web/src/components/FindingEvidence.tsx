// Each server's copy of a finding's stream or consumer: role, creation
// time, positions and configuration, with the odd values marked. Other
// entities have no copies, and nothing is drawn for them.
import type { ReactNode } from "react";
import { hasCopies, useCopies } from "../api/finding";
import type { ConsumerServerView, Entity, StreamServerView } from "../api/model";
import { fmtDuration, fmtInt, isoMinute } from "../lib/format";
import type { RangeSpec } from "../lib/timeRange";
import { serverPath } from "../lib/view";
import { Num, QueryView, RLink, Section } from "./common";

/** A copy created this much before or after the others is marked. */
const LATE_MS = 3_600_000;

type Copy = StreamServerView | ConsumerServerView;

interface Column {
  head: string;
  value: (c: Copy) => number | undefined;
}

const STREAM_COLS: Column[] = [
  { head: "Messages", value: (c) => (c as StreamServerView).messages },
  { head: "Last seq", value: (c) => (c as StreamServerView).lastSeq },
];

const CONSUMER_COLS: Column[] = [
  { head: "Delivered", value: (c) => (c as ConsumerServerView).deliveredStreamSeq },
  { head: "Ack floor", value: (c) => (c as ConsumerServerView).ackFloorStreamSeq },
];

function columns(kind: "stream" | "consumer"): Column[] {
  return kind === "stream" ? STREAM_COLS : CONSUMER_COLS;
}

function role(c: Copy): string {
  if (c.offline) return "offline";
  if (c.isLeader) return "leader";
  return c.current ? "follower" : "follower, behind";
}

/**
 * The value with the most matches by same(a, b), the smallest on a tie,
 * so the result does not depend on server order; unset unless it matches
 * more than half of values, so a split marks nothing.
 */
function majority<T>(values: T[], same: (a: T, b: T) => boolean, less: (a: T, b: T) => boolean): T | undefined {
  let best: T | undefined;
  let n = 0;
  for (const v of values) {
    const k = values.filter((u) => same(u, v)).length;
    if (k > n || (k === n && best !== undefined && less(v, best))) [best, n] = [v, k];
  }
  return n * 2 > values.length ? best : undefined;
}

const lessNum = (a: number, b: number) => a < b;

/** The creation time more than half the copies were created within LATE_MS of. */
export function commonCreated(times: number[]): number | undefined {
  return majority(times, (a, b) => Math.abs(a - b) <= LATE_MS, lessNum);
}

/** Positions that differ by at most max(100, 1%) are the same, as in the checks. */
export function samePosition(a: number, b: number): boolean {
  return Math.abs(a - b) <= Math.max(100, 0.01 * Math.max(Math.abs(a), Math.abs(b)));
}

/** The position more than half the values agree with. */
export function commonPosition(values: number[]): number | undefined {
  return majority(values, samePosition, lessNum);
}

/** The configuration more than half the reporting copies share. */
export function commonHash(copies: Copy[]): string | undefined {
  return majority(
    copies.map((c) => c.configHash).filter(Boolean),
    (a, b) => a === b,
    (a, b) => a < b,
  );
}

/** What each copy is compared with; an offline copy never sets a position. */
function references(kind: "stream" | "consumer", copies: Copy[]) {
  const created = copies.map((c) => c.created).filter((t): t is number => t !== undefined);
  const live = copies.filter((c) => !c.offline);
  const positions = columns(kind).map((col) => {
    const values = live.map(col.value).filter((v): v is number => v !== undefined);
    return { n: values.length, ref: commonPosition(values) };
  });
  const hashes = copies.filter((c) => c.configHash).length;
  return { created: { n: created.length, ref: commonCreated(created) }, positions, hash: { n: hashes, ref: commonHash(copies) } };
}

/** Whether a value that two or more copies report has no majority. */
export function copiesDisagree(kind: "stream" | "consumer", copies: Copy[]): boolean {
  const r = references(kind, copies);
  return [r.created, r.hash, ...r.positions].some((x) => x.n >= 2 && x.ref === undefined);
}

function Odd() {
  return (
    <>
      <span className="odd-mark" aria-hidden="true">
        {" "}
        ◆
      </span>
      <span className="sr-only"> differs from most copies</span>
    </>
  );
}

function Cell({ odd, children, className = "num" }: { odd: boolean; children: ReactNode; className?: string }) {
  return <td className={`${className}${odd ? " odd" : ""}`}>{children}</td>;
}

export function CopiesTable({ kind, copies }: { kind: "stream" | "consumer"; copies: Copy[] }) {
  const cols = columns(kind);
  const ref = references(kind, copies);
  const common = ref.created.ref;
  const hash = ref.hash.ref;
  return (
    <div className="card table-wrap scroll-cue">
      <table className="tbl" aria-label="Server copies">
        <thead>
          <tr>
            <th scope="col">Server</th>
            <th scope="col">Role</th>
            <th scope="col">Created</th>
            {cols.map((c) => (
              <th key={c.head} scope="col" className="num">
                {c.head}
              </th>
            ))}
            <th scope="col" className="num">Lag</th>
            <th scope="col">Config</th>
          </tr>
        </thead>
        <tbody>
          {copies.map((c) => {
            const late = c.created !== undefined && common !== undefined && Math.abs(c.created - common) > LATE_MS;
            const posOdd = cols.map((col, i) => {
              const v = col.value(c);
              const r = ref.positions[i].ref;
              // An offline copy's positions are stale; its role says so.
              return !c.offline && v !== undefined && r !== undefined && !samePosition(v, r);
            });
            const cfgOdd = hash !== undefined && !!c.configHash && c.configHash !== hash;
            const odd = late || cfgOdd || posOdd.some(Boolean);
            return (
              <tr key={c.server} className={odd ? "odd" : undefined}>
                <th scope="row" className="mono">
                  <RLink to={serverPath(c.server)}>{c.server}</RLink>
                </th>
                <td>{role(c)}</td>
                <Cell odd={late} className="mono">
                  {c.created === undefined ? <Num v={undefined} /> : `${isoMinute(c.created)} UTC`}
                  {late && <small>{c.created! > common! ? "+" : "−"}{fmtDuration(Math.abs(c.created! - common!))}</small>}
                </Cell>
                {cols.map((col, i) => (
                  <Cell key={col.head} odd={posOdd[i]}>
                    <Num v={col.value(c)} fmt={fmtInt} />
                    {posOdd[i] && <Odd />}
                  </Cell>
                ))}
                <td className="num">
                  <Num v={c.lag} />
                </td>
                <Cell odd={cfgOdd} className={cfgOdd ? "" : hash !== undefined ? "ok" : ""}>
                  {!c.configHash ? <Num v={undefined} /> : cfgOdd ? "differs" : hash !== undefined ? "same" : <span className="mono">{c.configHash.slice(0, 8)}</span>}
                </Cell>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

/** The copies of a stream or consumer finding; nothing for other entities. */
export function FindingEvidence({ spec, entity, state }: { spec: RangeSpec; entity: Entity | undefined; state: string }) {
  const q = useCopies(spec, entity, state);
  if (!hasCopies(entity)) return null;
  return (
    <Section title="Server copies" aside={`Each server's copy of the ${entity.kind}${q.data && copiesDisagree(q.data.kind, q.data.servers) ? " · copies disagree" : "."}`}
      right={
        <button type="button" className="btn" aria-label="Refresh server copies" onClick={() => q.refetch()} disabled={q.isFetching}>
          Refresh
        </button>
      }
    >
      <QueryView q={q} what={`the ${entity.kind}'s copies`}>
        {(d) => (d.servers.length ? <CopiesTable kind={d.kind} copies={d.servers} /> : <p className="muted-note">No server reports this {entity.kind}.</p>)}
      </QueryView>
    </Section>
  );
}
