// The Capacity page's server cards: each server's JetStream file store, how
// full it is, its growth and when it fills, and the finding that fires for
// it.
import { useCapacityPage } from "../lib/useCapacityPage";
import { CapacityHistoryChart, serverHistoryQuery, useCapacityHistory } from "./CapacityHistory";
import type { TrendSlice } from "../api/trendBatch";
import type { ServerStore } from "../api/capacity";
import { clusterGroups, growthLine, storeFindingText, storeTone, storeUse } from "../lib/capacityView";
import { fmtInt } from "../lib/format";
import { findingPath, serverPath } from "../lib/view";
import { RLink } from "./common";
import { Sev } from "./Severity";

function ServerCard({ s, history }: { s: ServerStore; history: TrendSlice }) {
  const use = storeUse(s);
  const tone = storeTone(s);
  const growth = s.perDay === undefined && s.reported ? <span title="This server lacks usable observations spanning 6 h">growth unavailable</span> : growthLine(s);
  return (
    <section className="card cap-server" aria-label={`Server ${s.name}`}>
      <div className="cap-server-h">
        <RLink to={serverPath(s.name)} className="x cap-server-name">
          {s.name}
        </RLink>
        <span className="cap-store">JetStream file store</span>
      </div>
      {!s.reported ? (
        <p className="cap-growth cap-quiet">not reported in this collection</p>
      ) : (
        <>
          {use && (
            <div className="cap-use">
              {use.used} <span className="cap-of">{use.of}</span>
            </div>
          )}
          {use?.pct !== undefined && (
            <div className="cap-bar" aria-hidden="true">
              <div className={`cap-bar-fill${use.pct > 0 ? " cap-bar-some" : ""}${tone ? ` cap-bar-${tone}` : ""}`} style={{ width: `${use.pct}%` }} />
            </div>
          )}
          {growth && <p className="cap-growth">{growth}</p>}
        </>
      )}
      <CapacityHistoryChart q={history} title={`${s.name} server file-store usage`} />
      {s.findingId && (
        <p className="cap-finding">
          <Sev level={s.findingSeverity ?? "info"} />{" "}
          <RLink to={findingPath(s.findingId)} className={`x${tone ? ` cap-${tone}` : ""}`}>
            {storeFindingText(s)}
          </RLink>
        </p>
      )}
    </section>
  );
}

export function ServerCards({ servers, withoutJetstream }: { servers: ServerStore[]; withoutJetstream: number }) {
  const [page, setPage] = useCapacityPage(servers.length, 24);
  const offset = page * 24;
  const shown = servers.slice(offset, offset + 24);
  const queries = shown.map(s => serverHistoryQuery(s.name));
  const history = useCapacityHistory(queries);
  const groups = clusterGroups(shown);
  const named = groups.length > 1;
  return (
    <div className="cap-servers">
      {servers.length === 0 && <p className="cap-line">No server with JetStream in this collection.</p>}
      {groups.map((g) => (
        <section key={g.cluster} className="cap-group" aria-label={named ? `Cluster ${g.cluster || "without a name"}` : "JetStream file stores"}>
          {named && <h2 className="cap-cluster">{g.cluster ? `Cluster ${g.cluster}` : "No cluster reported"}</h2>}
          <div className="cap-cards">
            {g.servers.map((s) => (
              <ServerCard key={s.name} s={s} history={history(serverHistoryQuery(s.name))} />
            ))}
          </div>
        </section>
      ))}
      {servers.length > 24 && <div className="cap-history-pages"><span>Server cards {offset + 1}–{Math.min(offset+24,servers.length)} of {servers.length}. Total history includes all servers.</span><button className="btn" disabled={offset===0} onClick={() => setPage(Math.max(0,page-1))}>Previous servers</button><button className="btn" disabled={offset+24>=servers.length} onClick={() => setPage(page+1)}>Next servers</button></div>}
      {withoutJetstream > 0 && (
        <p className="cap-line">
          {withoutJetstream === 1 ? "1 server" : `${fmtInt(withoutJetstream)} servers`} without JetStream {withoutJetstream === 1 ? "has" : "have"} no card.
        </p>
      )}
    </div>
  );
}
