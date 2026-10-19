// "Gone between A and B": the streams and durable consumers that servers
// held at A and none holds at B, with who held them, whether a delete was
// captured, and a link to compare the two moments in Placement.
import type { GoneAsset } from "../api/adapt-replay";
import { isoSecond } from "../lib/format";
import type { Moments } from "../lib/replayUrl";
import { comparePath, goneAtB, goneKind, gonePath, serversText } from "../lib/replayView";
import { entityName, eventPath } from "../lib/view";
import { RLink } from "./common";

export function ReplayGone({ gone, truncated, moments, read }: { gone: GoneAsset[]; truncated: boolean; moments: Moments; read: boolean }) {
  return (
    <section className="sec rp-gone">
      <div className="sec-h">
        <h2>Gone between A and B</h2>
        <div className="right">
          <RLink to={comparePath(moments)} className="btn">
            Compare in Placement
          </RLink>
        </div>
      </div>
      {gone.length === 0 ? (
        <div className="state-box empty">
          {read ? "No stream or durable consumer that servers held at A is missing at B." : "Not observed for this A and B: gone assets need a collection at each and a provider that reads them."}
        </div>
      ) : (
        <div className="card table-wrap">
          <table className="tbl">
            <thead>
              <tr>
                <th>Asset</th>
                <th>Kind</th>
                <th>Servers at A</th>
                <th>At B</th>
                <th>Delete event</th>
                <th>Last reported</th>
                <th className="num">Durables with it</th>
              </tr>
            </thead>
            <tbody>
              {gone.map((g) => {
                const p = gonePath(g, moments.a);
                const name = entityName(g.entity);
                return (
                  <tr key={g.entity.key}>
                    <td className="mono">
                      {p ? <RLink to={p}>{name}</RLink> : name}
                      {g.entity.kind === "consumer" && <small>on {g.entity.stream}</small>}
                    </td>
                    <td>{goneKind(g)}</td>
                    <td className="mono">{serversText(g.serversAtA)}</td>
                    <td className={`mono${g.notAnsweringAtB.length ? "" : " bad"}`}>{goneAtB(g)}</td>
                    <td>
                      {g.deleteEvent ? (
                        <>
                          <RLink to={eventPath(g.deleteEvent.id)}>{g.deleteEvent.summary || g.deleteEvent.kind}</RLink>
                          {g.deleteClient && <small>by {g.deleteClient}</small>}
                        </>
                      ) : (
                        "none captured"
                      )}
                    </td>
                    <td className="mono">{g.lastReported !== undefined ? `${isoSecond(g.lastReported)} UTC` : "not recorded"}</td>
                    <td className="num">{g.entity.kind === "consumer" ? "" : g.consumersWithIt}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
          {truncated && <p className="rp-foot">More assets went than the 200 listed.</p>}
        </div>
      )}
    </section>
  );
}
