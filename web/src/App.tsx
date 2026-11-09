import {ChartIntervalProvider} from "./charts/ChartIntervals";
import { InvestigationProvider } from "./components/InvestigationContext";
import { RouteBoundary } from "./components/RouteBoundary";
// The shell and the routes. Every page reads the time range, the system and
// the account scope from the URL; the sidebar's pages are listed in nav.ts.
import { Route, Routes, useLocation } from "react-router";
import { PageHistoryProvider } from "./components/HistoryNote";
import { Header } from "./components/Header";
import { Sidebar } from "./components/Sidebar";
import { StaleProvider } from "./components/StaleContext";
import { Landing, SystemGate } from "./components/SystemGate";
import { useRoutePosition } from "./lib/useRoutePosition";
import { useSidebar } from "./lib/useSidebar";
import { useSystem } from "./lib/useSystem";
import {MetricInvestigation} from "./pages/MetricInvestigation";
import { Fleet } from "./pages/Fleet";
import { SlowConsumerInvestigation } from "./pages/SlowConsumerInvestigation";
import { Overview } from "./pages/Overview";
import { Checks, CheckDetail } from "./pages/Checks";
import { Findings } from "./pages/Findings";
import { FindingByLabels } from "./pages/FindingByLabels";
import {TimelineEvents} from "./pages/TimelineEvents";
import {TimelineWindow} from "./pages/TimelineWindow";
import {FindingHistory} from "./pages/FindingHistory";
import {FindingOccurrence} from "./pages/FindingOccurrence";
import { FindingDetail } from "./pages/FindingDetail";
import { Events } from "./pages/Events";
import { Streams } from "./pages/Streams";
import { StreamDetail } from "./pages/StreamDetail";
import { Consumers } from "./pages/Consumers";
import { ConsumerDetail } from "./pages/ConsumerDetail";
import { Servers } from "./pages/Servers";
import { ServerDetail } from "./pages/ServerDetail";
import { Connections } from "./pages/Connections";
import { KvStores } from "./pages/KvStores";
import { ObjectStores } from "./pages/ObjectStores";
import { Placement } from "./pages/Placement";
import { Replay } from "./pages/Replay";
import { Topology } from "./pages/Topology";
import { Accounts } from "./pages/Accounts";
import { Capacity } from "./pages/Capacity";
import { StorageChange } from "./pages/StorageChange";
import { DataHistory } from "./pages/DataHistory";
import { Subjects } from "./pages/Subjects";
import { MicroServices } from "./pages/MicroServices";
import { Applications } from "./pages/Applications";
import { NotFound } from "./pages/NotFound";

export function App() {
  const { pathname, search, hash } = useLocation();
  const system = useSystem();
  const main = useRoutePosition();
  const { collapsed, setCollapsed, drawerOpen, setDrawerOpen, drawerShown } = useSidebar();
  return (
    <InvestigationProvider><ChartIntervalProvider><div className={`shell${collapsed ? " rail" : ""}`}>
      <a className="skip" href="#main">
        Skip to content
      </a>
      <Sidebar collapsed={collapsed} onCollapse={setCollapsed} drawerOpen={drawerOpen} onCloseDrawer={() => setDrawerOpen(false)} />
      {drawerOpen && <div className="side-backdrop" aria-hidden="true" onClick={() => setDrawerOpen(false)} />}
      {/* While the drawer is shown the page behind it is inert, so Tab stays in the drawer. */}
      <div className="main-col" inert={drawerShown}>
        <Header menuOpen={drawerOpen} onMenu={() => setDrawerOpen(!drawerOpen)} />
        <main className="wrap" id="main" tabIndex={-1} ref={main}>
          {/* Keyed by system too: a page of another system starts afresh, with
              no rows, filters or page tokens of the one before. */}
          <StaleProvider key={`${pathname}|${system}`}>
            <SystemGate>
              <PageHistoryProvider>
              <RouteBoundary key={pathname} href={pathname + search + hash}>
              <Routes>
                <Route
                  path="/"
                  element={
                    <Landing>
                      <Overview />
                    </Landing>
                  }
                />
                <Route path="/fleet" element={<Fleet />} />
                <Route path="/checks" element={<Checks />} />
                <Route path="/checks/:code" element={<CheckDetail />} />
                <Route path="/findings" element={<Findings />} />
                <Route path="/findings/history" element={<FindingHistory />} />
                <Route path="/findings/by-labels" element={<FindingByLabels />} />
                <Route path="/findings/:id/occurrences/:firstEpoch" element={<FindingOccurrence />} />
                <Route path="/findings/:id" element={<FindingDetail />} />
                <Route path="/timeline/events" element={<TimelineEvents />} />
                <Route path="/timeline/windows/:id" element={<TimelineWindow />} />
                <Route path="/events" element={<Events />} />
                <Route path="/streams" element={<Streams />} />
                <Route path="/streams/:account/:stream" element={<StreamDetail />} />
                <Route path="/consumers" element={<Consumers />} />
                <Route path="/consumers/:account/:stream/:consumer" element={<ConsumerDetail />} />
                <Route path="/servers" element={<Servers />} />
                <Route path="/servers/slow-consumers" element={<SlowConsumerInvestigation />} />
                <Route path="/servers/:name" element={<ServerDetail />} />
                <Route path="/subjects" element={<Subjects />} />
                <Route path="/micro-services" element={<MicroServices />} />
                <Route path="/applications" element={<Applications />} />
                <Route path="/connections" element={<Connections />} />
                <Route path="/kv-stores" element={<KvStores />} />
                <Route path="/object-stores" element={<ObjectStores />} />
                <Route path="/kv-stores/:account/:bucket" element={<StreamDetail />} />
                <Route path="/object-stores/:account/:bucket" element={<StreamDetail />} />
                <Route path="/placement" element={<Placement />} />
                <Route path="/replay" element={<Replay />} />
                <Route path="/topology" element={<Topology />} />
                <Route path="/accounts" element={<Accounts />} />
                <Route path="/metrics" element={<MetricInvestigation/>}/><Route path="/metrics/:family" element={<MetricInvestigation/>}/>
                <Route path="/capacity" element={<Capacity />} />
                <Route path="/capacity/storage-change" element={<StorageChange />} />
                <Route path="/data-history" element={<DataHistory />} />
                <Route path="*" element={<NotFound />} />
              </Routes>
              </RouteBoundary>
              </PageHistoryProvider>
            </SystemGate>
          </StaleProvider>
        </main>
      </div>
    </div></ChartIntervalProvider></InvestigationProvider>
  );
}
