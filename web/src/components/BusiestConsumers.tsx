import {useState} from "react";
// The consumers with the highest delivery rate at the range's end, with
// their backlog, unacknowledged messages and the delivery rate over the
// range, each read from the consumer's leader.
import type { ConsumerRate } from "../api/overview";
import { pickTrend, useTrends, type TrendSlice } from "../api/trendBatch";
import { MiniLine } from "../charts/MiniLine";
import { rangeText } from "../charts/chartLabel";
import type { RangeSpec } from "../lib/timeRange";
import { fmtCount } from "../lib/format";
import { fmtValue } from "../lib/units";
import { streamLabel, consumerPath, streamPath } from "../lib/view";
import { Num, RLink } from "./common";

const rate = (v: number) => fmtValue(v, "per_second");
// Two decimals below 10, where most consumers deliver: 0.12, not 0.1.
const perSec = (v: number) => (Math.abs(v) >= 10 ? fmtCount(v) : v.toFixed(2));

function Spark({ c, q, start, end, actionTarget }: { c: ConsumerRate; q: TrendSlice; start: number; end: number; actionTarget:HTMLElement|null }) {
  return <MiniLine actionTarget={actionTarget} investigation={{metric:SPARK_METRIC,entity:c.entity}} frame={{start,end,stepMs:q.data?.stepMs}} points={q.data?.points ?? []} level="ok" label={`${c.entity.consumer} delivered per second`} period={rangeText(start, end)} fmt={rate} />;
}

const SPARK_METRIC = "consumer.delivered_rate";

export function BusiestConsumers({ consumers, spec, start, end }: { consumers: ConsumerRate[]; spec: RangeSpec; start: number; end: number }) {
  const queries = consumers.map((c) => ({ metric: SPARK_METRIC, entity: c.entity }));
  const b = useTrends(spec, queries);
  return (
    <div className="table-wrap">
      <table className="tbl bc">
        <thead>
          <tr>
            <th scope="col">Consumer</th>
            <th scope="col" className="num">
              Delivered/s
            </th>
            <th scope="col" className="num">
              Pending
            </th>
            <th scope="col" className="num">
              Unacked
            </th>
            <th scope="col">Range</th>
          </tr>
        </thead>
        <tbody>
          {consumers.map((c) => (
            <ConsumerRow key={c.entity.key} c={c} q={pickTrend(b, queries, { metric: SPARK_METRIC, entity: c.entity })} start={start} end={end} />
          ))}
        </tbody>
      </table>
    </div>
  );
}

function ConsumerRow({c,q,start,end}:{c:ConsumerRate;q:TrendSlice;start:number;end:number}) {
  const [actionTarget,setActionTarget]=useState<HTMLTableCellElement|null>(null);
  return (
    <tr className="investigation-action-host">
      <td ref={setActionTarget} className="bc-consumer">
        <RLink to={consumerPath(c.entity.account, c.entity.stream, c.entity.consumer)} className="x mono" title={c.entity.consumer}>
          {c.entity.consumer}
        </RLink>
        <br />
        <RLink to={streamPath(c.entity.account, c.entity.stream)} className="x sub mono">
          {streamLabel(c.entity.stream)}
        </RLink>
      </td>
      <td className="num">
        <Num v={c.deliveredPerSecond} fmt={perSec} />
      </td>
      <td className="num">
        <Num v={c.numPending} />
      </td>
      <td className="num">
        <Num v={c.numAckPending} />
      </td>
      <td>
        <Spark c={c} q={q} actionTarget={actionTarget} start={start} end={end} />
      </td>
    </tr>
  );
}
