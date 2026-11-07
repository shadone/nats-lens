import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter } from "react-router";
import "@fontsource/ibm-plex-sans/latin-400.css";
import "@fontsource/ibm-plex-sans/latin-500.css";
import "@fontsource/ibm-plex-sans/latin-600.css";
import "@fontsource/ibm-plex-mono/latin-400.css";
import "@fontsource/ibm-plex-mono/latin-500.css";
import "@fontsource/ibm-plex-mono/latin-600.css";
import "./styles/index.css";
import { App } from "./App";
import { ClockGate } from "./components/ClockGate";
import { GlobalQueryProvider } from "./lib/useGlobalQuery";
import { shouldRetry } from "./api/errors";
import { clearLegacyIdentityFlag } from "./api/identity";

clearLegacyIdentityFlag();

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { staleTime: 10_000, retry: shouldRetry, refetchOnWindowFocus: false },
    mutations: { retry: false },
  },
});

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <ClockGate>
          <GlobalQueryProvider>
            <App />
          </GlobalQueryProvider>
        </ClockGate>
      </BrowserRouter>
    </QueryClientProvider>
  </StrictMode>,
);
