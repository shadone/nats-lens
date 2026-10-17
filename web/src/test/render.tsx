// Renders a component inside a router and a fresh query client, with a
// probe that shows the current location for URL state assertions.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import type { ReactElement } from "react";
import { MemoryRouter, useLocation, useNavigationType } from "react-router";

function LocationProbe() {
  const l = useLocation();
  const nav = useNavigationType();
  return (
    <>
      <div data-testid="location">{l.pathname + l.search}</div>
      <div data-testid="navigation">{nav}</div>
    </>
  );
}

export function renderApp(ui: ReactElement, route = "/") {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[route]}>
        {ui}
        <LocationProbe />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}
