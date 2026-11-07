import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import { Route, Routes } from "react-router";
import { useOverview, useSeries, useStream } from "../api/queries";
import { renderApp } from "../test/render";
import { StreamDetail } from "./StreamDetail";

vi.mock("../api/queries", () => ({ useOverview: vi.fn(), useSeries: vi.fn(), useStream: vi.fn() }));
vi.mock("../charts/SeriesChart", () => ({ QuerySeriesChart: ({ title }: { title: string }) => <div>{title}</div>, SeriesChart: ({ title }: { title: string }) => <div>{title}</div> }));
vi.mock("../components/EntityParts", () => ({ EntityFindings: () => <div>Findings</div>, EntityEvents: () => <div>Events</div>, Disagreements: () => null, ConsumersTable: () => <div>Consumers table</div>, Stat: ({ label, value }: { label: string; value: ReactNode }) => <div>{label}: {value}</div> }));

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

it("keeps stream diagnostics secondary and shows only reported bucket facts", () => {
  vi.mocked(useStream).mockReturnValue({ data: { stream: { entity: { kind: "stream", key: "APP/KV_preferences", account: "APP", stream: "KV_preferences" }, status: "ok", leader: "nats-0", replicas: 3 }, config: [{ key: "name", value: "KV_preferences" }, { key: "subjects", value: "$KV.preferences.>" }, { key: "max_age", value: "1d" }, { key: "max_msgs_per_subject", value: "5" }], servers: [], disagreements: [], consumers: [], findings: [] }, isPending: false, error: null } as never);
  page("/kv-stores/APP/preferences?range=6h&system=example-prod&account=APP");
  expect(screen.getByText("TTL: 1d")).toBeInTheDocument();
  expect(screen.getByText("History per key: 5")).toBeInTheDocument();
  expect(screen.getByText(/not a count of live keys/)).toBeInTheDocument();
  expect(screen.queryByText("$KV.preferences.>")).not.toBeInTheDocument();
  expect(screen.getByRole("link", { name: "Open stream diagnostics" })).toHaveAttribute("href", expect.stringContaining("diagnostics=stream"));
  expect(screen.queryByRole("heading", { name: "KV_preferences" })).not.toBeInTheDocument();
});
it("does not redirect explicit underlying-stream diagnostics", () => {
  vi.mocked(useStream).mockReturnValue({ data: { stream: { entity: { kind: "stream", account: "APP", stream: "KV_preferences" }, status: "ok", leader: "" }, config: [], servers: [], disagreements: [], consumers: [], findings: [] }, isPending: false, error: null } as never);
  page("/streams/APP/KV_preferences?diagnostics=stream");
  expect(screen.getByRole("heading", { level: 1, name: "KV_preferences" })).toBeInTheDocument();
  expect(screen.getByTestId("location")).toHaveTextContent("/streams/APP/KV_preferences?diagnostics=stream");
});
it("shows an explicitly unlimited TTL as none, without treating missing TTL as zero", () => {
  vi.mocked(useStream).mockReturnValue({ data: { stream: { entity: { kind: "stream", account: "APP", stream: "KV_preferences" }, status: "ok", leader: "" }, config: [{ key: "max_age", value: "0" }], servers: [], disagreements: [], consumers: [], findings: [] }, isPending: false, error: null } as never);
  page("/kv-stores/APP/preferences");
  expect(screen.getByText("TTL: none")).toBeInTheDocument();
});
