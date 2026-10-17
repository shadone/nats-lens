// Servers: every server seen in the range, with the ones expected and
// missing in the newest epoch.
import { useServers } from "../api/queries";
import { Empty, QueryView, RLink, Num } from "../components/common";
import { HealthIcon } from "../components/Severity";
import { fmtBytes, fmtCount, fmtDuration, hm } from "../lib/format";
import { usePageTitle } from "../lib/usePageTitle";
import { useTimeRange } from "../lib/useTimeRange";
import { serverPath } from "../lib/view";

export function Servers() {
  usePageTitle("Servers");
  const { spec } = useTimeRange();
  const q = useServers(spec);
  return (
    <>
      <div className="page-h">
        <h1>Servers</h1>
        <span className="aside">By stable server name; a restart gives a server a new ID.</span>
      </div>
      <QueryView q={q} what="servers">
        {(d) => (
          <>
            {d.missing.length > 0 && (
              <div className="state-box err" role="status">
                <b>Not answering:</b> {d.missing.join(", ")} did not answer in the newest epoch.
              </div>
            )}
            {d.servers.length === 0 ? (
              <Empty>No server reported in this range.</Empty>
            ) : (
              <section className="sec">
                <div className="card table-wrap">
                  <table className="tbl">
                    <thead>
                      <tr>
                        <th scope="col">
                          <span className="sr-only">Status</span>
                        </th>
                        <th scope="col">Server</th>
                        <th scope="col">Version</th>
                        <th scope="col" className="num">
                          Uptime
                        </th>
                        <th scope="col">Role</th>
                        <th scope="col">Healthz</th>
                        <th scope="col" className="num">
                          Connections
                        </th>
                        <th scope="col" className="num">
                          JetStream storage
                        </th>
                        <th scope="col" className="num">
                          Memory
                        </th>
                        <th scope="col" className="num">
                          CPU
                        </th>
                        <th scope="col">Restarts in range</th>
                      </tr>
                    </thead>
                    <tbody>
                      {d.servers.map((s) => (
                        <tr key={s.name}>
                          <td>
                            <HealthIcon level={s.status} />
                          </td>
                          <td className="mono">
                            <RLink to={serverPath(s.name)}>{s.name}</RLink>
                            {!s.present && <small className="bad">missing</small>}
                          </td>
                          <td className="mono">{s.version}</td>
                          <td className="num">{s.uptimeMs !== undefined ? fmtDuration(s.uptimeMs) : "–"}</td>
                          <td>{s.role}</td>
                          <td className={s.healthz === "ok" ? "ok" : s.healthz ? "bad" : undefined} title={s.healthzError || undefined}>
                            {s.healthz || "–"}
                          </td>
                          <td className="num"><Num v={s.connections} /></td>
                          <td className="num">
                            <Num v={s.jsStorageBytes} fmt={fmtBytes} />
                            {(s.jsMaxStorageBytes ?? 0) > 0 && <small>of <Num v={s.jsMaxStorageBytes} fmt={fmtBytes} /></small>}
                          </td>
                          <td className="num"><Num v={s.memBytes} fmt={fmtBytes} /></td>
                          <td className="num"><Num v={s.cpuPercent} fmt={(v) => `${fmtCount(v)}%`} /></td>
                          <td className="mono">{s.restarts.map(hm).join(", ") || "none"}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
                {d.expected.length > 0 && <p className="muted-note">Expected in the newest epoch: {d.expected.join(", ")}.</p>}
              </section>
            )}
          </>
        )}
      </QueryView>
    </>
  );
}
