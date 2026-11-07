// Synthetic browser fixture: Vite serves this entry, production builds do not.
import { useState } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter } from "react-router";
import { GlobalQueryProvider } from "../lib/useGlobalQuery";
import { RecentEvents } from "../components/ServerDetailParts";
import { rangeKey, type RangeSpec } from "../lib/timeRange";
import { EventRow } from "../components/EventRow";
import type { LensEvent } from "../api/model";
import "../styles/index.css";

const event: LensEvent = {
  id: "synthetic-selection", time: 1792247340000, source: "nats",
  kind: "consumer.deleted", type: "CONSUMER.DELETED",
  summary: "Synthetic consumer summary with words that wrap across several lines. " + "synthetic".repeat(40),
  server: "server-1", account: "shop", stream: "orders", consumer: "processor",
  clientName: "", clientHost: "", reason: "", raised: [], raw: "",
};
const spec: RangeSpec = { kind: "fixed", from: 1792243800000, to: 1792247400000 };
// No network requests: all fixture data stays synthetic and local.
const queryClient = new QueryClient({ defaultOptions: { queries: { enabled: false } } });
for (const system of ["", "shop"]) {
  queryClient.setQueryData(["serverEvents", system, rangeKey(spec), "server-1", 6], [{ ...event, repeats: 1 }]);
}
function Fixture() {
  const [open, setOpen] = useState(false);
  return <>
    <p id="elsewhere">Unrelated selectable text</p>
    <EventRow e={event} end={1792247400000} open={open} onToggle={() => setOpen(v => !v)} />
    <EventRow e={{ ...event, id: "synthetic-other" }} end={1792247400000} open={false} onToggle={() => {}} />
    <RecentEvents name="server-1" spec={spec} end={1792247400000} />
  </>;
}
createRoot(document.getElementById("root")!).render(
  <QueryClientProvider client={queryClient}><BrowserRouter><GlobalQueryProvider><Fixture /></GlobalQueryProvider></BrowserRouter></QueryClientProvider>,
);
