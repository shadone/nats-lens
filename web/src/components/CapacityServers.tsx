import { HistoryLink, useGrowthHistoryNote } from "./HistoryNote";
// The Capacity page's server cards: each server's JetStream file store, how
// full it is, its growth and when it fills, and the finding that fires for
// it.
import type { ServerStore } from "../api/capacity";
import { clusterGroups, growthLine, storeFindingText, storeTone, storeUse } from "../lib/capacityView";
import { fmtInt } from "../lib/format";
import { findingPath, serverPath } from "../lib/view";
import { RLink } from "./common";
import { Sev } from "./Severity";

function ServerCard({ s }: { s: ServerStore }) {
  const use = storeUse(s);
  const tone = storeTone(s);
  const missing = useGrowthHistoryNote();
  const growth = s.perDay === undefined && s.reported ? <HistoryLink>{missing ?? growthLine(s)}</HistoryLink> : growthLine(s);
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
  const groups = clusterGroups(servers);
  const named = groups.length > 1;
  return (
    <div className="cap-servers">
      {servers.length === 0 && <p className="cap-line">No server with JetStream in this collection.</p>}
      {groups.map((g) => (
        <section key={g.cluster} className="cap-group" aria-label={named ? `Cluster ${g.cluster || "without a name"}` : "JetStream file stores"}>
          {named && <h2 className="cap-cluster">{g.cluster ? `Cluster ${g.cluster}` : "No cluster reported"}</h2>}
          <div className="cap-cards">
            {g.servers.map((s) => (
              <ServerCard key={s.name} s={s} />
            ))}
          </div>
        </section>
      ))}
      {withoutJetstream > 0 && (
        <p className="cap-line">
          {withoutJetstream === 1 ? "1 server" : `${fmtInt(withoutJetstream)} servers`} without JetStream {withoutJetstream === 1 ? "has" : "have"} no card.
        </p>
      )}
    </div>
  );
}
