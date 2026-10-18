// The placement matrix: a row per stream with its consumers indented, a
// column per server. Each cell is a button that selects it.
import type { Matrix, MatrixAsset, MatrixCell, MatrixServer } from "../api/placementMatrix";
import { fmtBytes, fmtDuration, fmtInt } from "../lib/format";
import { CELL_LABEL, cellText, cellTone, isConsumer, momentText, roleLetter, type Change } from "../lib/placementView";
import { entityPath, serverPath } from "../lib/view";
import { RLink } from "./common";
import { PlacementMark } from "./PlacementSquares";

export interface CellRef {
  asset: string;
  server: string;
}

/** "leads 6 · 256 MiB · up 47h", with the meta leader and a server that did not answer. */
export function serverNote(s: MatrixServer): string {
  const parts: string[] = [];
  if (s.leads !== undefined) parts.push(`leads ${s.leads}`);
  if (s.metaLeader) parts.push("meta leader");
  if (s.memBytes !== undefined) parts.push(fmtBytes(s.memBytes));
  if (s.uptimeMs !== undefined) parts.push(`up ${fmtDuration(s.uptimeMs)}`);
  if (!s.present) parts.push("not answering");
  return parts.join(" · ");
}

/** "R3 · 7,378 msgs", "KV bucket · R1", "durable · R3 · 1 unplaced". */
export function assetMeta(a: MatrixAsset): string {
  const parts: string[] = [];
  const name = a.entity.stream;
  if (isConsumer(a)) parts.push("durable");
  else if (name.startsWith("KV_")) parts.push("KV bucket");
  else if (name.startsWith("OBJ_")) parts.push("object store");
  if (a.replicas !== undefined) parts.push(`R${a.replicas}`);
  if (a.messages !== undefined) parts.push(`${fmtInt(a.messages)} msgs`);
  if (a.unplaced > 0) parts.push(`${a.unplaced} unplaced`);
  return parts.join(" · ");
}

function cellLabel(a: MatrixAsset, c: MatrixCell, change: Change | undefined, compareAt: number | undefined): string {
  const differs = c.differs.length ? `, differs: ${c.differs.map((d) => (d === "config_hash" ? "configuration" : "creation time")).join(" and ")}` : "";
  const changed = change && compareAt !== undefined ? `, changed: ${CELL_LABEL[change.then]} at ${momentText(compareAt)}` : "";
  return `${a.entity.name} on ${c.server}: ${CELL_LABEL[c.state]}${differs}, ${cellText(a, c)}${changed}`;
}

function Row({
  a,
  servers,
  selected,
  onSelect,
  changes,
  compareAt,
}: {
  a: MatrixAsset;
  servers: MatrixServer[];
  selected?: CellRef;
  onSelect: (r: CellRef) => void;
  changes?: Map<string, Change>;
  compareAt?: number;
}) {
  const consumer = isConsumer(a);
  const path = entityPath(a.entity);
  const hot = a.matches && (a.serversDisagree || a.cells.some((c) => c.state === "disagrees"));
  return (
    <tr className={`${consumer ? "pm-c" : "pm-s"}${a.matches ? "" : " ctx"}${hot ? " hot" : ""}`}>
      <th scope="row" className="pm-asset">
        <span className="mono pm-n">{path ? <RLink to={path}>{a.entity.name}</RLink> : a.entity.name}</span>
        <small>{assetMeta(a)}</small>
      </th>
      {servers.map((s) => {
        const c = a.cells.find((x) => x.server === s.name);
        const change = changes?.get(`${a.entity.key}|${s.name}`);
        const mark = change && (
          <span className="pm-chg" title={compareAt !== undefined ? `${CELL_LABEL[change.then]} at ${momentText(compareAt)}` : undefined} aria-hidden="true" />
        );
        if (!c) {
          return (
            <td key={s.name}>
              <span className={`pm-cell t-none${change ? " changed" : ""}`}>
                <span aria-hidden="true">·</span>
                <span className="sr-only">not placed</span>
                {mark}
                {change && compareAt !== undefined && <span className="sr-only">{`, changed: ${CELL_LABEL[change.then]} at ${momentText(compareAt)}`}</span>}
              </span>
            </td>
          );
        }
        const on = selected?.asset === a.entity.key && selected.server === s.name;
        return (
          <td key={s.name}>
            <button
              type="button"
              className={`pm-cell t-${cellTone(c)}${on ? " on" : ""}${change ? " changed" : ""}`}
              aria-pressed={on}
              aria-label={cellLabel(a, c, change, compareAt)}
              onClick={() => onSelect({ asset: a.entity.key, server: s.name })}
            >
              <PlacementMark state={c.state} />
              <span className="pm-role mono" aria-hidden="true">
                {roleLetter(c)}
              </span>
              <span className="pm-txt mono">{cellText(a, c)}</span>
              {mark}
            </button>
          </td>
        );
      })}
    </tr>
  );
}

export function PlacementMatrix({
  m,
  selected,
  onSelect,
  changes,
  compareAt,
}: {
  m: Matrix;
  selected?: CellRef;
  onSelect: (r: CellRef) => void;
  changes?: Map<string, Change>;
  compareAt?: number;
}) {
  return (
    <section className="card pm-card" aria-label="Placement matrix">
      <div className="table-wrap">
        <table className="tbl pm">
          <caption className="sr-only">A row per stream, its consumers below it, and a column per server; select a cell for that server's view.</caption>
          <thead>
            <tr>
              <th scope="col" className="pm-asset">
                Asset
              </th>
              {m.servers.map((s) => (
                <th scope="col" key={s.name} className={s.present ? undefined : "gone"}>
                  <RLink to={serverPath(s.name)} className="x mono pm-sn">
                    {s.name}
                  </RLink>
                  <small>{serverNote(s)}</small>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {m.streams.flatMap((s) => [s, ...s.consumers]).map((a) => (
              <Row key={a.entity.key} a={a} servers={m.servers} selected={selected} onSelect={onSelect} changes={changes} compareAt={compareAt} />
            ))}
          </tbody>
        </table>
      </div>
      <ul className="pm-legend" aria-label="Matrix legend">
        <li>
          <b className="mono">L</b> leader · <b className="mono">F</b> follower
        </li>
        <li>
          <span className="pm-key t-ok" /> in sync
        </li>
        <li>
          <span className="pm-key t-behind" /> behind
        </li>
        <li>
          <span className="pm-key t-disagree" /> disagrees with the others
        </li>
        <li>
          <span className="pm-key t-missing" /> assigned but missing
        </li>
        <li>
          <span className="pm-key t-offline" /> offline
        </li>
        <li>
          <span className="pm-key t-none" /> not placed here (·)
        </li>
        {changes && (
          <li>
            <span className="pm-chg" /> changed since the moment compared
          </li>
        )}
        <li className="pl-note">Cell text: what differs, else the replica's lag</li>
      </ul>
    </section>
  );
}
