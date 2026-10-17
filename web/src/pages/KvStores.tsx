// KV Stores: the KV buckets of the newest epoch in the range, and those the
// inventory lists that no server reports, worst first.
import { useKeyValueStores, type KvBucket } from "../api/buckets";
import { BucketName, BytesOf, Notes, placementStates, statusOrder, Ttl } from "../components/BucketParts";
import { Dash, Empty, Num, QueryView } from "../components/common";
import { PlacementLegend, PlacementSquares } from "../components/PlacementSquares";
import { HealthIcon } from "../components/Severity";
import { SortTh } from "../components/SortHeader";
import { fmtGrowth } from "../lib/format";
import { sortRows, useSort, type SortValue } from "../lib/sort";
import { usePageTitle } from "../lib/usePageTitle";
import { useAccountScope } from "../lib/useSystem";
import { useTimeRange } from "../lib/useTimeRange";
import { accountLabeller } from "../lib/view";

const KEYS_NOTE = "subjects, including deleted keys";

const SORT: Record<string, (b: KvBucket) => SortValue> = {
  status: statusOrder,
  bucket: (b) => b.bucket,
  replicas: (b) => b.replicas,
  keys: (b) => b.keys,
  history: (b) => b.history,
  ttl: (b) => b.ttlMs,
  bytes: (b) => b.bytes,
  growth: (b) => b.bytesPerDay,
  watchers: (b) => b.watchers,
};

export function KvStores() {
  usePageTitle("KV Stores");
  const { spec } = useTimeRange();
  const [account] = useAccountScope();
  const [sort, by] = useSort();
  const q = useKeyValueStores(spec, account);
  return (
    <>
      <div className="page-h">
        <h1>KV Stores</h1>
        <span className="aside">KV buckets as the leader reports them in the newest epoch of the range. A dash means the value was not observed.</span>
      </div>
      <QueryView q={q} what="KV stores">
        {(all) => {
          if (all.length === 0) return <Empty>No KV buckets in the newest epoch of this range.</Empty>;
          const value = sort && SORT[sort.key];
          const rows = value ? sortRows(all, value, sort.dir) : all;
          const label = accountLabeller(all.map((b) => b.entity));
          const th = { sort, onSort: by };
          // Without an inventory no bucket is expected, and saying so on every row says nothing.
          const inventory = all.some((b) => b.expected);
          return (
            <section className="sec">
              <div className="card table-wrap">
                <table className="tbl">
                  <thead>
                    <tr>
                      <SortTh id="status" label="Status" srOnly first="asc" {...th} />
                      <SortTh id="bucket" label="Bucket" first="asc" {...th} />
                      <SortTh id="replicas" label="R" num {...th} />
                      <th scope="col">Placement</th>
                      <SortTh id="keys" label="Keys" num title={KEYS_NOTE} marker="*" {...th} />
                      <SortTh id="history" label="History" num {...th} />
                      <SortTh id="ttl" label="TTL" num {...th} />
                      <SortTh id="bytes" label="Bytes" num {...th} />
                      <SortTh id="growth" label="Per day" num {...th} />
                      <SortTh id="watchers" label="Watchers" num {...th} />
                      <th scope="col">Storage</th>
                      <th scope="col">Notes</th>
                    </tr>
                  </thead>
                  <tbody>
                    {rows.map((b) => (
                      <tr key={b.entity.key}>
                        <td>
                          <HealthIcon level={b.status} />
                        </td>
                        <BucketName b={b} label={label} />
                        <td className="num"><Num v={b.replicas} /></td>
                        <td><PlacementSquares placement={b.placement} /></td>
                        <td className="num"><Num v={b.keys} /></td>
                        <td className="num"><Num v={b.history} /></td>
                        <td className="num"><Ttl ms={b.ttlMs} /></td>
                        <BytesOf b={b} />
                        <td className="num"><Num v={b.bytesPerDay} fmt={fmtGrowth} /></td>
                        <td className="num"><Num v={b.watchers} /></td>
                        <td className="mono">{b.storage ? [b.storage, b.compression === "s2" && "s2"].filter(Boolean).join(", ") : <Dash />}</td>
                        <Notes b={b} notes={[b.allowMsgTtl && "per-key TTL", !!b.mirror && `mirror of ${b.mirror}`, inventory && b.present && !b.expected && "not in inventory"]} />
                      </tr>
                    ))}
                  </tbody>
                </table>
                <div className="st-foot">
                  <span>* Keys counts {KEYS_NOTE}.</span>
                  <PlacementLegend states={placementStates(all)} />
                </div>
              </div>
            </section>
          );
        }}
      </QueryView>
    </>
  );
}
