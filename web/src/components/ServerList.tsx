// The server list: grouped by cluster (flat with one cluster), problems
// first, "Needs attention" by default, a text filter by name, version or
// tag, and "Show N more" per group.
import { Fragment, useState } from "react";
import type { ServerRow } from "../api/servers";
import { fmtBytes, fmtCount, fmtDuration, fmtInt } from "../lib/format";
import { attentionReasons, groupByCluster, leaders, matchesText, median, storePct, STORE_ATTENTION_PCT } from "../lib/servers";
import { serverPath } from "../lib/view";
import { Dash, Num, RLink } from "./common";

/** Rows shown per group in "All" before "Show N more"; a flat list shows more. */
const PER_GROUP = 6;
const PER_FLAT = 20;
const COLS = 11;

type Show = "attention" | "all";

function clusterSummary(rows: ServerRow[]): string {
  const up = rows.filter((s) => s.present).length;
  const down = rows.length - up;
  const cpu = median(rows.filter((s) => s.present && s.cpuPercent !== undefined).map((s) => s.cpuPercent!));
  const conns = rows.filter((s) => s.present && s.connections !== undefined);
  const store = median(rows.map(storePct).filter((v): v is number => v !== undefined));
  return [
    `${rows.length} ${rows.length === 1 ? "server" : "servers"}`,
    down ? `${up} answering, ${down} did not answer` : "all answering",
    cpu !== undefined ? `CPU median ${fmtCount(Math.round(cpu))}%` : "",
    conns.length ? `${fmtInt(conns.reduce((a, s) => a + s.connections!, 0))} connections` : "",
    store !== undefined ? `store median ${Math.round(store)}%` : "",
  ]
    .filter(Boolean)
    .join(" · ");
}

function StateCell({ reasons, present }: { reasons: string[]; present: boolean }) {
  const cls = !present ? "down" : reasons.length ? "warn" : "up";
  return (
    <td className={`srv-state ${cls}`}>
      <span className={`sq ${cls}`} aria-hidden="true" />
      {reasons.length ? reasons.join(" · ") : "ok"}
    </td>
  );
}

function StoreCell({ s }: { s: ServerRow }) {
  const p = storePct(s);
  if (p === undefined) return <td><Dash /></td>;
  return (
    <td className="srv-store" title={s.jsStorageBytes !== undefined && s.jsMaxStorageBytes ? `${fmtBytes(s.jsStorageBytes)} of ${fmtBytes(s.jsMaxStorageBytes)}` : undefined}>
      <span className="srv-bar" aria-hidden="true">
        <span className={p >= STORE_ATTENTION_PCT ? "warn" : undefined} style={{ width: `${Math.min(100, p).toFixed(0)}%` }} />
      </span>
      <span className="mono">{Math.round(p)}%</span>
    </td>
  );
}

function Row({ s, majority }: { s: ServerRow; majority?: string }) {
  const reasons = attentionReasons(s, majority);
  const off = majority !== undefined && s.version !== majority;
  const lead = leaders(s);
  return (
    <tr className={!s.present ? "row-bad" : reasons.length ? "row-warn" : undefined}>
      <td className="mono">
        <RLink to={serverPath(s.name)}>{s.name}</RLink>
      </td>
      <StateCell reasons={reasons} present={s.present} />
      <td>{s.present ? s.role : <Dash />}</td>
      <td className={`mono${off ? " warn" : ""}`}>{s.version || <Dash />}</td>
      <td className="num">{s.present && s.uptimeMs !== undefined ? fmtDuration(s.uptimeMs) : <Dash />}</td>
      <td className="num">{s.present ? <Num v={s.cpuPercent} fmt={(v) => `${fmtCount(v)}%`} /> : <Dash />}</td>
      <td className="num">{s.present ? <Num v={s.memBytes} fmt={fmtBytes} /> : <Dash />}</td>
      <td className="num">{s.present ? <Num v={s.connections} /> : <Dash />}</td>
      <td className="num" title={lead !== undefined ? `${s.streamLeaders ?? "–"} stream, ${s.consumerLeaders ?? "–"} consumer` : undefined}>
        <Num v={lead} />
      </td>
      {s.present ? <StoreCell s={s} /> : <td><Dash /></td>}
      <td className="mono tags">{s.tags.join(", ") || <Dash />}</td>
    </tr>
  );
}

export function ServerList({ servers, majority }: { servers: ServerRow[]; majority?: string }) {
  const [show, setShow] = useState<Show>("attention");
  const [text, setText] = useState("");
  const [opened, setOpened] = useState<Set<string>>(new Set());
  const attention = servers.filter((s) => attentionReasons(s, majority).length > 0);
  const pool = (show === "attention" ? attention : servers).filter((s) => matchesText(s, text));
  const all = groupByCluster(servers, majority);
  const flat = all.length <= 1;
  const groups = groupByCluster(pool, majority);
  const limit = show === "all" ? (flat ? PER_FLAT : PER_GROUP) : Infinity;
  return (
    <section className="sec srv-list" aria-label="Server list">
      <div className="srv-tools">
        <input type="search" aria-label="Filter servers" placeholder="Filter by name, version or tag" value={text} onChange={(e) => setText(e.target.value)} />
        <div className="chips" role="group" aria-label="Show">
          <button type="button" className={`chip${show === "attention" ? " on" : ""}`} aria-pressed={show === "attention"} onClick={() => setShow("attention")}>
            Needs attention {attention.length}
          </button>
          <button type="button" className={`chip${show === "all" ? " on" : ""}`} aria-pressed={show === "all"} onClick={() => setShow("all")}>
            All {servers.length}
          </button>
        </div>
        <span className="aside">Problems first, then by name</span>
      </div>
      {groups.length === 0 ? (
        <p className="card srv-none">{show === "attention" && !text ? "No server needs attention." : "No server matches."}</p>
      ) : (
        <div className="card table-wrap scroll-cue">
          <table className="tbl srv-tbl">
            <thead>
              <tr>
                <th scope="col">Server</th>
                <th scope="col">State</th>
                <th scope="col">Role</th>
                <th scope="col">Version</th>
                <th scope="col" className="num">Up</th>
                <th scope="col" className="num">CPU</th>
                <th scope="col" className="num">Memory</th>
                <th scope="col" className="num">Conns</th>
                <th scope="col" className="num">Leaders</th>
                <th scope="col">Store</th>
                <th scope="col">Tags</th>
              </tr>
            </thead>
            <tbody>
              {groups.map((g) => {
                const key = g.cluster || "-";
                const open = opened.has(key);
                const rows = open ? g.rows : g.rows.slice(0, limit);
                const more = g.rows.length - rows.length;
                const whole = all.find((a) => a.cluster === g.cluster)?.rows ?? g.rows;
                return (
                  <Fragment key={key}>
                    {!flat && (
                      <tr className="grp">
                        <th scope="rowgroup" colSpan={COLS}>
                          <span className="grp-name">{g.cluster || "no cluster"}</span>
                          <span className="grp-sum">{clusterSummary(whole)}</span>
                        </th>
                      </tr>
                    )}
                    {rows.map((s) => (
                      <Row key={s.name} s={s} majority={majority} />
                    ))}
                    {more > 0 && (
                      <tr className="more">
                        <td colSpan={COLS}>
                          <button type="button" className="linkish" onClick={() => setOpened(new Set([...opened, key]))}>
                            Show {more} more{flat ? "" : ` in ${g.cluster}`}
                          </button>
                        </td>
                      </tr>
                    )}
                  </Fragment>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
      <p className="muted-note srv-foot">Leaders are the stream and consumer leaders on the server.</p>
    </section>
  );
}
