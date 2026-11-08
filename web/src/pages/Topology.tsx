import { deferredView } from "../components/deferredView";
// Topology: how the servers of a system connect in the range's newest
// collection. A map of the clusters, their gateways and leaf node hubs,
// then the leaf node sites worst first and the routes inside one cluster.
// The cluster of the routes table lives in the URL as ?cluster=.
import { useSearchParams } from "react-router";
import { useTopology } from "../api/queries";
import { summary } from "../lib/topologyView";
import { usePageTitle } from "../lib/usePageTitle";
import { useTimeRange } from "../lib/useTimeRange";

const CLUSTER_PARAM = "cluster";

function useTopologyModel() {
  usePageTitle("Topology");
  const [params, setParams] = useSearchParams();
  const { spec } = useTimeRange();
  const q = useTopology(spec);
  const t = q.data;
  const line = t?.collected ? summary(t) : "";

  const pick = (c: string) =>
    setParams(
      (p) => {
        const n = new URLSearchParams(p);
        n.set(CLUSTER_PARAM, c);
        return n;
      },
      { replace: true },
    );
  return { params, q: { ...q }, t, line, pick };
}
export type TopologyModel = ReturnType<typeof useTopologyModel>;
const View = deferredView(() => import("../views/Topology").then(m => m.TopologyView));
export function Topology() {
  const model = useTopologyModel();
  return <View model={model} />;
}
