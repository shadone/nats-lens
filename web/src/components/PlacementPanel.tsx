// The side panel of the Placement page: one server's view of one asset,
// against the other servers', what its state means and how to check it by
// hand. It shows commands to copy; nats-lens runs none of them.
import type { ReactNode } from "react";
import type { MatrixAsset, MatrixCell } from "../api/placementMatrix";
import { fmtDuration, fmtInt, isoSecond } from "../lib/format";
import { useQuery } from "@tanstack/react-query";
import { shellQuery } from "../api/shell";
import { useSystem } from "../lib/useSystem";
import { CELL_LABEL, contextNote, infoCommand, isConsumer, meaning, momentText, peersAgree, stepDownCommand, type Change } from "../lib/placementView";
import { entityPath } from "../lib/view";
import { Dash, RLink } from "./common";
import { CopyCommand } from "./CopyCommand";
import { PlacementMark } from "./PlacementSquares";

/** A server's position: last and first sequence of a stream, delivered and ack floor of a consumer. */
function position(a: MatrixAsset, c: MatrixCell): string | undefined {
  const parts = isConsumer(a)
    ? [c.delivered !== undefined && `delivered ${fmtInt(c.delivered)}`, c.ackFloor !== undefined && `ack floor ${fmtInt(c.ackFloor)}`]
    : [c.lastSeq !== undefined && `last seq ${fmtInt(c.lastSeq)}`, c.firstSeq !== undefined && `first seq ${fmtInt(c.firstSeq)}`];
  const out = parts.filter(Boolean).join(", ");
  return out || undefined;
}

/** Whether every server that reports a position reports this one's. */
function positionsAgree(a: MatrixAsset): { agree: boolean; reporting: number } {
  const ps = a.cells.map((c) => position(a, c)).filter((p): p is string => p !== undefined);
  return { agree: ps.every((p) => p === ps[0]), reporting: ps.length };
}

function Row({ term, children, warn }: { term: string; children: ReactNode; warn?: boolean }) {
  return (
    <>
      <dt>{term}</dt>
      <dd className={`mono${warn ? " warn" : ""}`}>{children}</dd>
    </>
  );
}

export function PlacementPanel({
  a,
  c,
  now,
  change,
  compareAt,
  onClose,
}: {
  a: MatrixAsset;
  c: MatrixCell;
  /** The collection read, for "how long ago". */
  now: number;
  change?: Change;
  compareAt?: number;
  onClose: () => void;
}) {
  const page = useSystem();
  // The shell's status call names the default system; it is the shell's
  // own query, so this asks nothing more.
  const shell = useQuery(shellQuery(page));
  const system = page || shell.data?.defaultSystem || "";
  const consumer = isConsumer(a);
  const others = a.cells.filter((x) => x.server !== c.server);
  const pos = position(a, c);
  const agree = positionsAgree(a);
  const path = entityPath(a.entity);
  const eventsQuery = `${consumer ? "consumer" : "stream"}=${encodeURIComponent(a.entity.key)}`;
  const differs = c.differs.map((d) => (d === "config_hash" ? "configuration" : "creation time"));
  const peers = c.peers.length ? [c.server, ...c.peers].sort() : [];
  return (
    <aside className="card pm-panel" aria-labelledby="pm-panel-h">
      <div className="pm-panel-top">
        <span className="lbl">Selected</span>
        <button type="button" className="btn pm-close" onClick={onClose} aria-label="Close the selected cell">
          Close
        </button>
      </div>
      <h2 id="pm-panel-h" className="mono" tabIndex={-1}>
        {a.entity.name}
      </h2>
      <div className="pm-sub">
        {consumer && (
          <>
            on <span className="mono">{a.entity.stream}</span> ·{" "}
          </>
        )}
        seen from <span className="mono">{c.server}</span>
      </div>
      <div className="pm-state">
        <PlacementMark state={c.state} />
        <b>{CELL_LABEL[c.state]}</b>
        {differs.length > 0 && <span className="warn">· {differs.join(" and ")} differs from the other servers</span>}
      </div>
      <dl className="pm-dl">
        <Row term="Created" warn={c.differs.includes("created")}>
          {c.created !== undefined ? `${isoSecond(c.created)} UTC` : "not reported"}
        </Row>
        {others.map((o) => (
          <Row key={o.server} term={`${o.server} says`} warn={o.differs.includes("created")}>
            {o.created !== undefined ? `${isoSecond(o.created)} UTC` : CELL_LABEL[o.state]}
            {o.state === "leader" && " · leader"}
          </Row>
        ))}
        <Row term="Position">{pos ?? <Dash />}</Row>
        {agree.reporting > 1 && (
          <Row term="Positions" warn={!agree.agree}>
            {agree.agree
              ? agree.reporting === 2
                ? "equal on both servers that report one"
                : `equal on all ${agree.reporting} servers that report one`
              : others
                  .map((o) => `${o.server}: ${position(a, o) ?? "none"}`)
                  .join("; ")}
          </Row>
        )}
        {c.lag !== undefined && <Row term="Lag">{`${fmtInt(c.lag)} raft entries, as the leader reports`}</Row>}
        <Row term="Peers">
          {peers.length ? `${peers.join(" ")} ${peersAgree(a) ? "(all agree)" : "(servers name different replicas)"}` : "names none"}
        </Row>
        <Row term="Since">
          <Since a={a} c={c} now={now} change={change} compareAt={compareAt} />
        </Row>
        {c.configHash && <Row term="Config hash" warn={c.differs.includes("config_hash")}>{c.configHash}</Row>}
      </dl>
      <div className="pm-mean">
        <b>What it means</b>
        <p>{meaning(a, c)}</p>
      </div>
      <CopyCommand label="Check by hand" command={infoCommand(a, c)} note={contextNote(a, system)} />
      <div className="pm-links">
        <RLink to={`/events?${eventsQuery}`} className="btn">
          Events for this {consumer ? "consumer" : "stream"}
        </RLink>
        {path && (
          <RLink to={path} className="btn">
            Open the {consumer ? "consumer" : "stream"}
          </RLink>
        )}
      </div>
      {a.leader && (
        <>
          <CopyCommand label={`Step down leader ${a.leader}`} command={stepDownCommand(a)} button="Copy command" note={contextNote(a, system)} />
          <p className="pm-note">nats-lens changes nothing in NATS: it copies the command, and you decide whether to run it.</p>
        </>
      )}
    </aside>
  );
}

/** How long the state has lasted, as far as the data says. */
function Since({ a, c, now, change, compareAt }: { a: MatrixAsset; c: MatrixCell; now: number; change?: Change; compareAt?: number }) {
  const parts: string[] = [];
  if (c.state === "leader" && a.leaderSince !== undefined) parts.push(`leads since ${isoSecond(a.leaderSince)} UTC, ${fmtDuration(now - a.leaderSince)}`);
  if (c.differs.includes("created") && c.created !== undefined) parts.push(`its copy dates from ${fmtDuration(now - c.created)} ago`);
  if (compareAt !== undefined) {
    parts.push(change ? `${change.newAsset ? "not in the matrix" : CELL_LABEL[change.then]} at ${momentText(compareAt)}` : `the same at ${momentText(compareAt)}`);
  }
  return <>{parts.length ? parts.join("; ") : "not recorded; compare with an earlier time to see"}</>;
}
