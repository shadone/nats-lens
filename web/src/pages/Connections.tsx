import { deferredView } from "../components/deferredView";
// Connections: client connections of the newest epoch in the range, by
// server and connection ID, filtered by server, account and name.
import { useState, type FormEvent } from "react";
import { useSearchParams } from "react-router";
import { useConnectionFindings } from "../api/connectionFindings";
import { useConnections } from "../api/queries";
import { usePager } from "../components/Pager";
import { rangeKey } from "../lib/timeRange";
import { usePageTitle } from "../lib/usePageTitle";
import { useSystem } from "../lib/useSystem";
import { useResolvedRange, useTimeRange } from "../lib/useTimeRange";

function useConnectionsModel() {
  usePageTitle("Connections");
  const [params, setParams] = useSearchParams();
  const { spec } = useTimeRange();
  const { end } = useResolvedRange(spec);
  const server = params.get("server") ?? "";
  const account = params.get("account") ?? "";
  const name = params.get("name") ?? "";
  const tooling = params.get("tooling") === "1";
  const [nameText, setNameText] = useState(name);
  const [accountText, setAccountText] = useState(account);
  const pager = usePager([useSystem(), server, account, name, String(tooling), rangeKey(spec)].join("|"));
  // A name filter shows the tooling connections it matches: whoever types
  // a name looks for that connection.
  const q = useConnections(spec, { server, account, nameContains: name, includeTooling: tooling || name !== "", pageSize: 100, pageToken: pager.token });
  const findings = useConnectionFindings(spec, q.isPlaceholderData ? undefined : q.data?.epoch);
  const findingData = !q.isPlaceholderData && !findings.error ? findings.data : undefined;
  // The server filter's options come with the connections: every server
  // seen in the range, whatever the filters. The chosen server stays on
  // offer while they load, when the call fails, or when the range lacks it.
  const known = q.data?.servers ?? [];
  const servers = server && !known.includes(server) ? [...known, server] : known;
  const set = (entries: [string, string][]) =>
    setParams((p) => {
      const n = new URLSearchParams(p);
      for (const [k, v] of entries) {
        if (v) n.set(k, v);
        else n.delete(k);
      }
      return n;
    });
  const apply = (e: FormEvent) => {
    e.preventDefault();
    set([
      ["name", nameText.trim()],
      ["account", accountText.trim()],
    ]);
  };
  return { end, server, name, tooling, nameText, setNameText, accountText, setAccountText, pager, q: { ...q }, findings: { ...findings }, findingData, servers, set, apply };
}
export type ConnectionsModel = ReturnType<typeof useConnectionsModel>;
const View = deferredView(() => import("../views/Connections").then(m => m.ConnectionsView));
export function Connections() {
  const model = useConnectionsModel();
  return <View model={model} />;
}
