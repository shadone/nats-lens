// The consumers with the highest delivery rate at the range's end, with
// their backlog, unacknowledged messages and the delivery rate over the
// range, each read from the consumer's leader.
import type { ConsumerRate } from "../api/overview";
import { useTrend } from "../api/overview";
import { MiniLine } from "../charts/MiniLine";
import { rangeText } from "../charts/chartLabel";
import type { RangeSpec } from "../lib/timeRange";
import { fmtCount } from "../lib/format";
import { fmtValue } from "../lib/units";
import { consumerPath, streamPath } from "../lib/view";
import { Num, RLink } from "./common";

const rate = (v: number) => fmtValue(v, "per_second");
// Two decimals below 10, where most consumers deliver: 0.12, not 0.1.
const perSec = (v: number) => (Math.abs(v) >= 10 ? fmtCount(v) : v.toFixed(2));

function Spark({ c, spec, start, end }: { c: ConsumerRate; spec: RangeSpec; start: number; end: number }) {
  const q = useTrend(spec, "consumer.delivered_rate", c.entity);
  return <MiniLine points={q.data?.points ?? []} level="ok" label={`${c.entity.consumer} delivered per second`} period={rangeText(start, end)} fmt={rate} />;
}

export function BusiestConsumers({ consumers, spec, start, end }: { consumers: ConsumerRate[]; spec: RangeSpec; start: number; end: number }) {
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
            <tr key={c.entity.key}>
              <td>
                <RLink to={consumerPath(c.entity.account, c.entity.stream, c.entity.consumer)} className="x mono">
                  {c.entity.consumer}
                </RLink>
                <br />
                <RLink to={streamPath(c.entity.account, c.entity.stream)} className="x sub mono">
                  {c.entity.stream}
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
                <Spark c={c} spec={spec} start={start} end={end} />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
