// The shell and the routes. Every page reads the time range, the system and
// the account scope from the URL; the sidebar's pages are listed in nav.ts.
import { Route, Routes, useLocation } from "react-router";
import { Header } from "./components/Header";
import { Sidebar } from "./components/Sidebar";
import { StaleProvider } from "./components/StaleContext";
import { Landing, SystemGate } from "./components/SystemGate";
import { useSidebar } from "./lib/useSidebar";
import { useSystem } from "./lib/useSystem";
import { Fleet } from "./pages/Fleet";
import { Overview } from "./pages/Overview";
import { Findings } from "./pages/Findings";
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
import { NotFound } from "./pages/NotFound";

export function App() {
  const { pathname } = useLocation();
  const system = useSystem();
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
        <main className="wrap" id="main" tabIndex={-1}>
          {/* Keyed by system too: a page of another system starts afresh, with
              no rows, filters or page tokens of the one before. */}
          <StaleProvider key={`${pathname}|${system}`}>
            <SystemGate>
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
                <Route path="/findings" element={<Findings />} />
                <Route path="/findings/:id" element={<FindingDetail />} />
                <Route path="/events" element={<Events />} />
                <Route path="/streams" element={<Streams />} />
                <Route path="/streams/:account/:stream" element={<StreamDetail />} />
                <Route path="/consumers" element={<Consumers />} />
                <Route path="/consumers/:account/:stream/:consumer" element={<ConsumerDetail />} />
                <Route path="/servers" element={<Servers />} />
                <Route path="/servers/:name" element={<ServerDetail />} />
                <Route path="/connections" element={<Connections />} />
                <Route path="/kv-stores" element={<KvStores />} />
                <Route path="/object-stores" element={<ObjectStores />} />
                <Route path="*" element={<NotFound />} />
              </Routes>
            </SystemGate>
          </StaleProvider>
        </main>
      </div>
    </div>
  );
}
