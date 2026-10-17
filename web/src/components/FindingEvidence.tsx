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

function role(c: Copy): string {
  if (c.offline) return "offline";
  if (c.isLeader) return "leader";
  return c.current ? "follower" : "follower, behind";
}

/**
 * The creation time most copies share: the one with the most copies
 * created within LATE_MS of it, the earliest on a tie.
 */
export function commonCreated(times: number[]): number | undefined {
  let best: number | undefined;
  let n = 0;
  for (const t of [...times].sort((a, b) => a - b)) {
    const near = times.filter((u) => Math.abs(u - t) <= LATE_MS).length;
    if (near > n) [best, n] = [t, near];
  }
  return best;
}

/** The configuration most copies share; "" when none reports one. */
export function majorityHash(copies: Copy[]): string {
  const counts = new Map<string, number>();
  for (const c of copies) if (c.configHash) counts.set(c.configHash, (counts.get(c.configHash) ?? 0) + 1);
  let best = "";
  let n = 0;
  for (const [h, k] of counts) if (k > n) [best, n] = [h, k];
  return best;
}

function Cell({ odd, children, className = "num" }: { odd: boolean; children: ReactNode; className?: string }) {
  return <td className={`${className}${odd ? " odd" : ""}`}>{children}</td>;
}

export function CopiesTable({ kind, copies }: { kind: "stream" | "consumer"; copies: Copy[] }) {
  const cols = kind === "stream" ? STREAM_COLS : CONSUMER_COLS;
  const leader = copies.find((c) => c.isLeader);
  const created = copies.map((c) => c.created).filter((t): t is number => t !== undefined);
  const common = commonCreated(created);
  const hash = majorityHash(copies);
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
            const posOdd = cols.map((col) => !!leader && leader !== c && col.value(c) !== undefined && col.value(leader) !== undefined && col.value(c) !== col.value(leader));
            const cfgOdd = !!hash && !!c.configHash && c.configHash !== hash;
            const odd = late || cfgOdd || posOdd.some(Boolean);
            return (
              <tr key={c.server} className={odd ? "odd" : undefined}>
                <td className="mono">
                  <RLink to={serverPath(c.server)}>{c.server}</RLink>
                </td>
                <td>{role(c)}</td>
                <Cell odd={late} className="mono">
                  {c.created === undefined ? <Num v={undefined} /> : `${isoMinute(c.created)} UTC`}
                  {late && <small>{c.created! > common! ? "+" : "−"}{fmtDuration(Math.abs(c.created! - common!))}</small>}
                </Cell>
                {cols.map((col, i) => (
                  <Cell key={col.head} odd={posOdd[i]}>
                    <Num v={col.value(c)} fmt={fmtInt} />
                  </Cell>
                ))}
                <td className="num">
                  <Num v={c.lag} />
                </td>
                <Cell odd={cfgOdd} className={cfgOdd ? "" : "ok"}>
                  {!c.configHash ? <Num v={undefined} /> : cfgOdd ? "differs" : "same"}
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
export function FindingEvidence({ spec, entity }: { spec: RangeSpec; entity: Entity | undefined }) {
  const q = useCopies(spec, entity);
  if (!hasCopies(entity)) return null;
  return (
    <Section title="Server copies" aside={`Each server's copy of the ${entity.kind}.`}>
      <QueryView q={q} what={`the ${entity.kind}'s copies`}>
        {(d) => (d.servers.length ? <CopiesTable kind={d.kind} copies={d.servers} /> : <p className="muted-note">No server reports this {entity.kind}.</p>)}
      </QueryView>
    </Section>
  );
}
