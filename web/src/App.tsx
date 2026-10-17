// Routes. Every page reads the global time range from the URL.
import { Route, Routes, useLocation } from "react-router";
import { Header } from "./components/Header";
import { StaleProvider } from "./components/StaleContext";
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
import { NotFound } from "./pages/NotFound";

export function App() {
  const { pathname } = useLocation();
  return (
    <>
      <a className="skip" href="#main">
        Skip to content
      </a>
      <Header />
      <main className="wrap" id="main" tabIndex={-1}>
        <StaleProvider key={pathname}>
          <Routes>
            <Route path="/" element={<Overview />} />
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
            <Route path="*" element={<NotFound />} />
          </Routes>
        </StaleProvider>
      </main>
    </>
  );
}
