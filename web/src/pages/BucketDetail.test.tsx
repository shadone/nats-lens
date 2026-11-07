import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import { Route, Routes } from "react-router";
import { useOverview, useSeries, useStream } from "../api/queries";
import { renderApp } from "../test/render";
import { StreamDetail } from "./StreamDetail";

vi.mock("../api/queries", () => ({ useOverview: vi.fn(), useSeries: vi.fn(), useStream: vi.fn() }));
vi.mock("../charts/SeriesChart", () => ({ QuerySeriesChart: ({ title }: { title: string }) => <div>{title}</div>, SeriesChart: ({ title }: { title: string }) => <div>{title}</div> }));
vi.mock("../components/EntityParts", () => ({ EntityFindings: () => <div>Findings</div>, EntityEvents: () => <div>Events</div>, Disagreements: () => null, ConsumersTable: () => <div>Consumers table</div>, Stat: ({ label, value }: { label: string; value: unknown }) => <div>{label}: {String(value)}</div> }));

function page(route: string) {
  return renderApp(<Routes><Route path="/streams/:account/:stream" element={<StreamDetail />} />
    <Route path="/kv-stores/:account/:bucket" element={<StreamDetail />} />
    <Route path="/object-stores/:account/:bucket" element={<StreamDetail />} /></Routes>, route);
}
beforeEach(() => {
  vi.mocked(useOverview).mockReturnValue({ data: { windows: [] } } as never);
  vi.mocked(useSeries).mockReturnValue({ data: undefined } as never);
});
describe("bucket detail identity", () => {
  it.each([["KV_partner-info", "partner-info", "KV Stores", "/kv-stores"], ["OBJ_product-images", "product-images", "Object Stores", "/object-stores"]])("presents legacy %s as a bucket with scoped canonical links", (stream, bucket, list, prefix) => {
    vi.mocked(useStream).mockReturnValue({ data: { stream: { entity: { kind: "stream", key: `APP/${stream}`, account: "APP", stream }, status: "ok", leader: "", replicas: 3 }, config: [], servers: [], disagreements: [], consumers: [], findings: [] }, isPending: false, error: null } as never);
    page(`/streams/APP/${stream}?range=6h&system=example-prod&account=APP`);
    expect(screen.getByRole("heading", { level: 1, name: bucket })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: list })).toHaveAttribute("href", expect.stringContaining(prefix));
    const location = screen.getByTestId("location").textContent!;
    expect(location).toContain(`${prefix}/APP/${bucket}`);
    expect(location).toContain("account=APP");
    expect(screen.getByText(/does not read.*contents/i)).toBeInTheDocument();
  });
});
