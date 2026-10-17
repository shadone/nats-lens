import { create, fromJson } from "@bufbuild/protobuf";
import { timestampFromMs } from "@bufbuild/protobuf/wkt";
import { screen, waitFor, within } from "@testing-library/react";
import { Route, Routes } from "react-router";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { lens } from "../api/client";
import { EventSource, ListEventsResponseSchema } from "../gen/natslens/v1/events_pb";
import { ListFindingsResponseSchema } from "../gen/natslens/v1/findings_pb";
import { GetServerResponseSchema, type GetServerResponse } from "../gen/natslens/v1/servers_pb";
import { GetTimeSeriesResponseSchema, Unit } from "../gen/natslens/v1/timeseries_pb";
import { exampleGlobalAp3, exampleGlobalUs4 } from "../test/fixtures/exampleGlobal";
import { renderApp } from "../test/render";
import { ServerDetail } from "./ServerDetail";

vi.mock("../api/client", () => ({ lens: { getServer: vi.fn(), getTimeSeries: vi.fn(), listEvents: vi.fn(), listFindings: vi.fn() } }));
const m = vi.mocked(lens);

const now = Date.now();
const points = (values: number[]) => values.map((value, i) => ({ time: timestampFromMs(now - (values.length - i) * 600_000), value }));

beforeEach(() => {
  vi.resetAllMocks();
  m.getTimeSeries.mockImplementation(async (req) => {
    const v = req.metric === "server.cpu_percent" ? [6, 8, 10, 8] : req.metric === "server.connections" ? [31, 31, 31] : [];
    return create(GetTimeSeriesResponseSchema, {
      series: { metric: req.metric, unit: Unit.COUNT, points: points(v), min: v.length ? Math.min(...v) : undefined, max: v.length ? Math.max(...v) : undefined },
    });
  });
  m.listFindings.mockResolvedValue(create(ListFindingsResponseSchema, {}));
  m.listEvents.mockResolvedValue(
    create(ListEventsResponseSchema, {
      events: [
        { id: "1", time: timestampFromMs(now - 60_000), source: EventSource.KUBERNETES, kind: "k8s.pod.scheduled", summary: "pod scheduled on gke-example-pool-ap-3f89", repeats: 1 },
        { id: "2", time: timestampFromMs(now - 120_000), source: EventSource.NATS, kind: "client.connect", summary: "label-printer reconnected", repeats: 4 },
      ],
    }),
  );
});

function show(r: GetServerResponse, name: string) {
  m.getServer.mockResolvedValue(r);
  renderApp(
    <Routes>
      <Route path="/servers/:name" element={<ServerDetail />} />
    </Routes>,
    `/servers/${name}`,
  );
}

const ap3 = () => fromJson(GetServerResponseSchema, exampleGlobalAp3);

describe("ServerDetail on example-global", () => {
  it("shows five metric boxes with the value, the range and the limit in the note", async () => {
    show(ap3(), "nats-ap-3");
    expect(await screen.findByRole("heading", { name: "nats-ap-3" })).toBeInTheDocument();
    for (const t of ["CPU", "Memory", "Connections", "Messages in / out", "Slow consumers"]) expect(screen.getByRole("button", { name: `About ${t}` })).toBeInTheDocument();
    expect(await screen.findByRole("img", { name: /^CPU, current 8%, min 6%, max 10%/ })).toBeInTheDocument();
    expect(screen.getByText("steady at 31 all window")).toBeInTheDocument();
    expect(screen.getByText("limit 65,536")).toBeInTheDocument();
  });

  it("marks the setting that differs from most servers", async () => {
    show(ap3(), "nats-ap-3");
    const limits = await screen.findByRole("region", { name: "Limits and settings" });
    const payload = within(limits).getByText("Max payload").nextElementSibling!;
    expect(payload).toHaveClass("differs");
    expect(payload).toHaveTextContent("8 MiB");
    expect(payload).toHaveTextContent("differs from most servers");
    expect(within(limits).getByText("Max pending").nextElementSibling).not.toHaveClass("differs");
    expect(within(limits).getByText("1 setting differs from most servers.")).toBeInTheDocument();
    expect(within(limits).getByText("Ping").nextElementSibling).toHaveTextContent("every 2m · closes after 2 unanswered pings");
  });

  it("shows a dash for unreported pings and connection limit, never 0", async () => {
    const r = ap3();
    r.limits!.maxPingsOut = undefined;
    r.limits!.maxConnections = undefined;
    show(r, "nats-ap-3");
    const limits = await screen.findByRole("region", { name: "Limits and settings" });
    expect(within(limits).getByText("Ping").nextElementSibling).toHaveTextContent(/every 2m · closes after – (not observed )?unanswered pings/);
    const conns = within(limits).getAllByText("Connections").find((e) => e.tagName === "DT")!;
    expect(conns.nextElementSibling).toHaveTextContent(/^–/);
    expect(screen.queryByText("limit 0")).toBeNull();
  });

  it("shows where it runs, recent events and the endpoints that answered", async () => {
    show(ap3(), "nats-ap-3");
    const where = await screen.findByRole("region", { name: "Where it runs" });
    expect(where).toHaveTextContent("nats-ap-3 in nats");
    expect(where).toHaveTextContent("gke-example-pool-ap-3f89");
    expect(where).toHaveTextContent("ap-1a");
    const recent = screen.getByRole("region", { name: "Recent events" });
    expect(await within(recent).findByText("label-printer reconnected")).toBeInTheDocument();
    expect(recent).toHaveTextContent("×4");
    expect(screen.getByText("13/13")).toBeInTheDocument();
    const routes = screen.getByRole("region", { name: "Routes" });
    expect(routes).toHaveTextContent("5, slowest to nats-ap-5 0.47 ms");
  });

  it("asks for the server's events with routine ones hidden and repeats folded", async () => {
    show(ap3(), "nats-ap-3");
    await screen.findByRole("region", { name: "Recent events" });
    const req = m.listEvents.mock.calls[0][0];
    expect([req.hideRoutine, req.foldRepeats, req.entity?.server]).toEqual([true, true, "nats-ap-3"]);
  });
});

describe("ServerDetail, failing cases", () => {
  it("hides where it runs without the Kubernetes watcher", async () => {
    const r = ap3();
    r.kubernetes = undefined;
    show(r, "nats-ap-3");
    await screen.findByRole("region", { name: "Limits and settings" });
    expect(screen.queryByRole("region", { name: "Where it runs" })).toBeNull();
  });

  it("shows a dash, never a stale value, for a server that did not answer", async () => {
    show(fromJson(GetServerResponseSchema, exampleGlobalUs4), "nats-us-4");
    const h = await screen.findByRole("heading", { name: "nats-us-4" });
    expect(h.parentElement).toHaveTextContent(/did not answer in the newest epoch$/);
    const cpu = screen.getByRole("button", { name: "About CPU" }).closest(".mbox")!;
    await waitFor(() => expect(cpu.querySelector(".mb-v b")).toHaveTextContent("–"));
    expect(cpu).not.toHaveTextContent("5%");
    expect(cpu.querySelector(".mb-dot")).toBeNull();
    expect(cpu.querySelector(".mb-range")).not.toBeNull();
    expect(cpu).toHaveTextContent("no current value");
    expect(cpu).not.toHaveTextContent("no data");
  });
});
