// Object Stores: the object stores of the newest epoch in the range, and
// those the inventory lists that no server reports, worst first.
import { useObjectStores, type ObjectBucket } from "../api/buckets";
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

const MESSAGES_NOTE = "chunks plus one metadata message per object";

const SORT: Record<string, (b: ObjectBucket) => SortValue> = {
  status: statusOrder,
  bucket: (b) => b.bucket,
  replicas: (b) => b.replicas,
  messages: (b) => b.chunks,
  ttl: (b) => b.ttlMs,
  bytes: (b) => b.bytes,
  growth: (b) => b.bytesPerDay,
};

export function ObjectStores() {
  usePageTitle("Object Stores");
  const { spec } = useTimeRange();
  const [account] = useAccountScope();
  const [sort, by] = useSort();
  const q = useObjectStores(spec, account);
  return (
    <>
      <div className="page-h">
        <span className="aside">Object stores as the leader reports them in the newest collection of the range. A dash means the value was not observed.</span>
      </div>
      <QueryView q={q} what="object stores">
        {(all) => {
          if (all.length === 0) return <Empty>No object stores in the newest collection of this range.</Empty>;
          // Own keys only: ?sort=__proto__ must not reach the prototype.
          const value = sort && Object.hasOwn(SORT, sort.key) ? SORT[sort.key] : undefined;
          const rows = value && sort ? sortRows(all, value, sort.dir) : all;
          const label = accountLabeller(all.map((b) => b.entity));
          const th = { sort, onSort: by };
          const inventory = all.some((b) => b.expected);
          return (
            <section className="sec">
              <div className="card table-wrap scroll-cue">
                <table className="tbl">
                  <thead>
                    <tr>
                      <SortTh id="status" label="Status" srOnly first="asc" {...th} />
                      <SortTh id="bucket" label="Bucket" first="asc" {...th} />
                      <SortTh id="replicas" label="R" num {...th} />
                      <th scope="col">Placement</th>
                      <SortTh id="messages" label="Messages" num title={MESSAGES_NOTE} marker="*" {...th} />
                      <SortTh id="bytes" label="Bytes" num {...th} />
                      <SortTh id="growth" label="Per day" num {...th} />
                      <SortTh id="ttl" label="TTL" num {...th} />
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
                        <td className="pl-cell"><PlacementSquares placement={b.placement} /></td>
                        <td className="num"><Num v={b.chunks} /></td>
                        <BytesOf b={b} />
                        <td className="num"><Num v={b.bytesPerDay} fmt={fmtGrowth} /></td>
                        <td className="num"><Ttl ms={b.ttlMs} /></td>
                        <td className="mono">{b.storage || <Dash />}</td>
                        <Notes b={b} notes={[b.sealed && "sealed", inventory && b.present && !b.expected && "not in inventory"]} />
                      </tr>
                    ))}
                  </tbody>
                </table>
                <div className="st-foot">
                  <span>* Messages: {MESSAGES_NOTE}.</span>
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
