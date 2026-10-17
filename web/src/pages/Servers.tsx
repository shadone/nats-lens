// Servers: data points first, then charts across all servers, a map of
// every server, and the list grouped by cluster with problems first.
import { useServerList, type ServerRow } from "../api/servers";
import { Empty, QueryView } from "../components/common";
import { FleetCharts } from "../components/FleetCharts";
import { ServerGrid } from "../components/ServerGrid";
import { ServerHeadline } from "../components/ServerHeadline";
import { ServerList } from "../components/ServerList";
import { majorityVersion } from "../lib/servers";
import { usePageTitle } from "../lib/usePageTitle";
import { useResolvedRange, useTimeRange } from "../lib/useTimeRange";

function statusLine(servers: ServerRow[]): string {
  const clusters = new Set(servers.map((s) => s.cluster)).size;
  const leader = servers.find((s) => s.present && s.role === "meta leader")?.name;
  const count = `${servers.length} ${servers.length === 1 ? "server" : "servers"}${clusters > 1 ? ` in ${clusters} clusters` : ""}`;
  return [count, leader ? `meta leader ${leader}` : ""].filter(Boolean).join(" · ");
}

export function Servers() {
  usePageTitle("Servers");
  const { spec } = useTimeRange();
  const { start, end } = useResolvedRange(spec);
  const q = useServerList(spec);
  const servers = q.data?.servers ?? [];
  return (
    <>
      <div className="page-h">
        {servers.length > 0 && <span className="aside">{statusLine(servers)}</span>}
      </div>
      <QueryView q={q} what="servers">
        {(d) =>
          d.servers.length === 0 ? (
            <Empty>No server reported in this range.</Empty>
          ) : (
            <>
              <ServerHeadline list={d} spec={spec} start={start} end={end} />
              <FleetCharts spec={spec} start={start} end={end} />
              <ServerGrid servers={d.servers} />
              <ServerList servers={d.servers} majority={majorityVersion(d.servers)} />
            </>
          )
        }
      </QueryView>
    </>
  );
}
