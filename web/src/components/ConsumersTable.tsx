import type { ConsumerSummary } from "../api/model";
import { fmtDuration } from "../lib/format";
import { consumerPath } from "../lib/view";
import { Dash, Num, RLink } from "./common";
import { HealthIcon } from "./Severity";

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

