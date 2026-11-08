import { lazy } from "react";
import { useCheckDefinitions } from "./api/checks";
import { useStreams } from "./api/queries";
import { useTimeRange } from "./lib/useTimeRange";
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
import { Fleet } from "./pages/Fleet";
import { NotFound } from "./pages/NotFound";

const Checks = lazy(() => import("./routes/guidance").then(module => ({ default: module.Checks })));
const CheckDetail = lazy(() => import("./routes/guidance").then(module => ({ default: module.CheckDetail })));
const Findings = lazy(() => import("./routes/guidance").then(module => ({ default: module.Findings })));
const FindingByLabels = lazy(() => import("./routes/guidance").then(module => ({ default: module.FindingByLabels })));
const FindingDetail = lazy(() => import("./routes/guidance").then(module => ({ default: module.FindingDetail })));
const Streams = lazy(() => import("./routes/resources").then(module => ({ default: module.Streams })));
const StreamDetail = lazy(() => import("./routes/resourceDetails").then(module => ({ default: module.StreamDetail })));
const Consumers = lazy(() => import("./routes/resources").then(module => ({ default: module.Consumers })));
const ConsumerDetail = lazy(() => import("./routes/resourceDetails").then(module => ({ default: module.ConsumerDetail })));
const KvStores = lazy(() => import("./routes/resources").then(module => ({ default: module.KvStores })));
const ObjectStores = lazy(() => import("./routes/resources").then(module => ({ default: module.ObjectStores })));
const Servers = lazy(() => import("./routes/infrastructure").then(module => ({ default: module.Servers })));
const ServerDetail = lazy(() => import("./routes/infrastructure").then(module => ({ default: module.ServerDetail })));
const Placement = lazy(() => import("./routes/infrastructure").then(module => ({ default: module.Placement })));
const Topology = lazy(() => import("./routes/infrastructure").then(module => ({ default: module.Topology })));
const Accounts = lazy(() => import("./routes/infrastructure").then(module => ({ default: module.Accounts })));
const Capacity = lazy(() => import("./routes/infrastructure").then(module => ({ default: module.Capacity })));
const Events = lazy(() => import("./routes/activity").then(module => ({ default: module.Events })));
const Replay = lazy(() => import("./routes/activity").then(module => ({ default: module.Replay })));
const DataHistory = lazy(() => import("./routes/activity").then(module => ({ default: module.DataHistory })));
const Subjects = lazy(() => import("./routes/messaging").then(module => ({ default: module.Subjects })));
const MicroServices = lazy(() => import("./routes/messaging").then(module => ({ default: module.MicroServices })));
const Applications = lazy(() => import("./routes/messaging").then(module => ({ default: module.Applications })));
const Connections = lazy(() => import("./routes/messaging").then(module => ({ default: module.Connections })));
const Overview = lazy(() => import("./routes/overview").then(module => ({ default: module.Overview })));

// These primary-page reads commit outside Suspense, alongside the code request.
// The page hooks join the same cache entry rather than issuing a second read.
function CheckRead() { useCheckDefinitions(); return null; }
function StreamRead() {
  const { search } = useLocation();
  const { spec } = useTimeRange();
  useStreams(spec, new URLSearchParams(search).get("kv") === "1");
  return null;
}

export function App() {
  const { pathname, search, hash } = useLocation();
  const system = useSystem();
  const main = useRoutePosition();
  const { collapsed, setCollapsed, drawerOpen, setDrawerOpen, drawerShown } = useSidebar();
  return (
    <div className={`shell${collapsed ? " rail" : ""}`}>
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
              {pathname === "/streams" && <StreamRead />}
              {/^\/checks(?:\/|$)/.test(pathname) && <CheckRead />}
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
                <Route path="/findings/by-labels" element={<FindingByLabels />} />
                <Route path="/findings/:id" element={<FindingDetail />} />
                <Route path="/events" element={<Events />} />
                <Route path="/streams" element={<Streams />} />
                <Route path="/streams/:account/:stream" element={<StreamDetail />} />
                <Route path="/consumers" element={<Consumers />} />
                <Route path="/consumers/:account/:stream/:consumer" element={<ConsumerDetail />} />
                <Route path="/servers" element={<Servers />} />
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
                <Route path="/capacity" element={<Capacity />} />
                <Route path="/data-history" element={<DataHistory />} />
                <Route path="*" element={<NotFound />} />
              </Routes>
              </RouteBoundary>
              </PageHistoryProvider>
            </SystemGate>
          </StaleProvider>
        </main>
      </div>
    </div>
  );
}
