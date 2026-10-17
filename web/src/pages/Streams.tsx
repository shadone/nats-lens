// Streams: every stream of the newest epoch in the range, worst first.
import { useSearchParams } from "react-router";
import { useStreams } from "../api/queries";
import { MiniLine } from "../charts/MiniLine";
import { Dash, Empty, QueryView, RLink, Num } from "../components/common";
import { HealthIcon } from "../components/Severity";
import { fmtBytes, fmtGrowth } from "../lib/format";
import { usePageTitle } from "../lib/usePageTitle";
import { useTimeRange } from "../lib/useTimeRange";
import { AccountName } from "../components/AccountName";
import { accountLabeller, consumerCount, serverPath, streamPath } from "../lib/view";

export function Streams() {
  usePageTitle("Streams");
  const [params, setParams] = useSearchParams();
  const kv = params.get("kv") === "1";
  const { spec } = useTimeRange();
  const q = useStreams(spec, kv);
  return (
    <>
      <div className="page-h">
        <h1>Streams</h1>
        <span className="aside">As the leader reports them in the newest epoch of the range. Growth covers the whole range, scaled to a day.</span>
        <div className="right">
          <label className="check chip">
            <input
              type="checkbox"
              checked={kv}
              onChange={(e) =>
                setParams((p) => {
                  const n = new URLSearchParams(p);
                  if (e.target.checked) n.set("kv", "1");
                  else n.delete("kv");
                  return n;
                })
              }
            />{" "}
            Include KV and object stores
          </label>
        </div>
      </div>
      <QueryView q={q} what="streams">
        {(d) =>
          d.streams.length === 0 ? (
            <Empty>No streams in the newest epoch of this range.</Empty>
          ) : (
            <section className="sec">
              <div className="card table-wrap">
                <table className="tbl">
                  <thead>
                    <tr>
                      <th scope="col">
                        <span className="sr-only">Status</span>
                      </th>
                      <th scope="col">Stream</th>
                      <th scope="col" className="num">
                        R
                      </th>
                      <th scope="col">Leader</th>
                      <th scope="col" className="num">
                        Messages
                      </th>
                      <th scope="col" className="num">
                        Bytes
                      </th>
                      <th scope="col" className="num">
                        Consumers
                      </th>
                      <th scope="col" className="num">
                        7 days
                      </th>
                      <th scope="col" className="num">
                        Per day
                      </th>
                      <th scope="col">Findings</th>
                    </tr>
                  </thead>
                  <tbody>
                    {d.streams.map((s, _i, all) => {
                      const label = accountLabeller(all.map((x) => x.entity));
                      const { text: cons, short } = consumerCount(s);
                      return (
                        <tr key={s.entity.key}>
                          <td>
                            <HealthIcon level={s.status} />
                          </td>
                          <td className="mono">
                            <RLink to={streamPath(s.entity.account, s.entity.stream)}>{s.entity.stream}</RLink>
                            <small>
                              <AccountName id={s.entity.account} name={s.entity.accountName} label={label(s.entity.account, s.entity.accountName)} />
                              {s.serversDisagree && " · servers disagree"}
                            </small>
                          </td>
                          <td className="num"><Num v={s.replicas} /></td>
                          <td className="mono">{s.leader ? <RLink to={serverPath(s.leader)}>{s.leader}</RLink> : <Dash />}</td>
                          <td className="num"><Num v={s.messages} /></td>
                          <td className="num"><Num v={s.bytes} fmt={fmtBytes} /></td>
                          <td className={`num${short ? " bad" : ""}`}>{cons}</td>
                          <td className="num">
                            <MiniLine points={s.bytes7d?.points ?? []} level={s.status} label={`${s.entity.stream} bytes`} period="last 7 days" fmt={fmtBytes} />
                          </td>
                          <td className="num"><Num v={s.bytesPerDay} fmt={fmtGrowth} /></td>
                          <td className="mono">{s.findingCodes.join(", ")}</td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
                <div className="st-foot">
                  <span>Total <Num v={d.totals.bytes} fmt={fmtBytes} /> stored</span>
                  <span>{d.totals.durableConsumers} durable consumers</span>
                  <span>{d.totals.ephemeralConsumers} ephemeral or ordered</span>
                  {d.streams.some((s) => s.consumersExpected !== undefined) && <span>2/3 = present / expected by inventory</span>}
                </div>
              </div>
            </section>
          )
        }
      </QueryView>
    </>
  );
}
