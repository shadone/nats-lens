// Synthetic browser fixture. Uses the real request adapters, hooks and page;
// production builds do not include this entry. Never contacts a NATS deployment.
import { create } from "@bufbuild/protobuf";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter } from "react-router";
import { lens } from "../api/client";
import { ListConnectionsResponseSchema } from "../gen/natslens/v1/servers_pb";
import { EntityType, Severity } from "../gen/natslens/v1/common_pb";
import { FindingState, ListFindingsResponseSchema } from "../gen/natslens/v1/findings_pb";
import { GlobalQueryProvider } from "../lib/useGlobalQuery";
import { Connections } from "../pages/Connections";
import "../styles/index.css";

const counts = { connections: 0, findings: 0 };
Object.assign(window, { fixtureRequests: counts });
lens.listConnections = async req => {
  counts.connections++;
  return create(ListConnectionsResponseSchema, {
    epoch: 7n, totalCount: 101, nextPageToken: req.pageToken ? "" : "page-2", servers: ["nats-0"],
    connections: Array.from({ length: req.pageToken ? 1 : 100 }, (_, i) => ({
      key: `nats-0/${i}`, cid: BigInt(i), server: "nats-0", name: `orders-service@orders-service-${i}`,
      lang: "go", version: "1.49.0", account: "APP", ip: "192.0.2.10", rtt: { seconds: 0n, nanos: 180_000_000 },
    })),
  });
};
lens.listFindings = async () => {
  counts.findings++;
  return create(ListFindingsResponseSchema, { totalCount: 2, findings: [
    { id: "client-001:orders-service", checkCode: "client-001", checkName: "High client round trip time", severity: Severity.WARNING, state: FindingState.FIRING, entity: { type: EntityType.CLIENT, key: "orders-service" } },
    { id: "client-002:orders-service@orders-service-0", checkCode: "client-002", checkName: "High pending bytes", severity: Severity.WARNING, state: FindingState.RESOLVED, entity: { type: EntityType.CLIENT, key: "orders-service@orders-service-0" } },
  ] });
};
const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity, refetchOnWindowFocus: false } } });
createRoot(document.getElementById("root")!).render(
  <QueryClientProvider client={queryClient}><BrowserRouter><GlobalQueryProvider><main><h1>Connections</h1><Connections /></main></GlobalQueryProvider></BrowserRouter></QueryClientProvider>,
);
