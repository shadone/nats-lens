import { ListTraversal, useListTraversal } from "../components/ListTraversal";
import { useSystem } from "../lib/useSystem";
import { rangeKey } from "../lib/timeRange";
// Consumers: backlog and status per consumer, worst first.
import { useSearchParams } from "react-router";
import { useConsumers, useStreamNames } from "../api/queries";
import { Empty, QueryView } from "../components/common";
import { ConsumersTable } from "../components/EntityParts";
import { usePageTitle } from "../lib/usePageTitle";
import { useAccountScope } from "../lib/useSystem";
import { useResolvedRange, useTimeRange } from "../lib/useTimeRange";
import { accountLabeller } from "../lib/view";

export function Consumers() {
  usePageTitle("Consumers");
  const [params, setParams] = useSearchParams();
  const { spec } = useTimeRange();
  const { end } = useResolvedRange(spec);
  const eph = params.get("ephemeral") === "1";
  const stream = params.get("stream") ?? "";
  const [scope] = useAccountScope();
  // A chosen stream names its own account; without one the account scope applies.
  const [account = "", streamName = ""] = stream ? stream.split("/") : [scope];
  const streams = useStreamNames(spec).data ?? [];
  const streamLabel = accountLabeller(streams);
  // The chosen stream stays on offer while the streams load, when the call
  // fails, or when the range lacks it.
  const chosenMissing = stream !== "" && !streams.some((s) => `${s.account}/${s.stream}` === stream);
  const sys = useSystem();
  const traversal = useListTraversal(JSON.stringify([sys, rangeKey(spec), account, streamName, eph]));
  const q = useConsumers(spec, { account, stream: streamName, includeEphemeral: eph }, traversal.token);
  const set = (k: string, v: string) =>
    setParams((p) => {
      const n = new URLSearchParams(p);
      if (v) n.set(k, v);
      else n.delete(k);
      return n;
    });
  return (
    <>
      <div className="page-h">
        <span className="aside">Backlog from the leader's view in the newest collection of the range. A dash means the value was not observed.</span>
      </div>
      <div className="filters">
        <label className="grp">
          <span className="lbl">Stream</span>
          <select value={stream} onChange={(e) => set("stream", e.target.value)}>
            <option value="">All streams</option>
            {streams.map((s) => (
              <option key={s.key} value={`${s.account}/${s.stream}`}>
                {`${s.stream} (${streamLabel(s.account, s.accountName)})`}
              </option>
            ))}
            {chosenMissing && <option value={stream}>{`${streamName} (${account})`}</option>}
          </select>
        </label>
        <label className="check">
          <input type="checkbox" checked={eph} onChange={(e) => set("ephemeral", e.target.checked ? "1" : "")} /> Include ephemeral and ordered
        </label>
      </div>
      <ListTraversal traversal={traversal} next={q.data?.nextPageToken} count={q.data?.totalCount} loaded={q.data?.length ?? 0} error={q.error} refresh={() => { if (!traversal.token) void q.refetch(); }} />
      <QueryView q={q} what="consumers">
        {(cs) => (cs.length ? <ConsumersTable consumers={cs} end={end} /> : <Empty>No consumers match in the newest collection of this range.</Empty>)}
      </QueryView>
    </>
  );
}
