import { create, fromJson } from "@bufbuild/protobuf";
import { timestampFromMs } from "@bufbuild/protobuf/wkt";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Route, Routes } from "react-router";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { lens } from "../api/client";
import { EventSource, ListEventsResponseSchema } from "../gen/natslens/v1/events_pb";
import { ListFindingsResponseSchema } from "../gen/natslens/v1/findings_pb";
import { GetServerResponseSchema, ServerRole, type GetServerResponse } from "../gen/natslens/v1/servers_pb";
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
    expect(screen.getAllByRole("heading", { level: 1 }).map((h) => h.textContent)).toEqual(["nats-ap-3"]);
    for (const t of ["CPU", "Memory", "Connections", "Messages", "Slow-consumer detections"]) expect(screen.getByRole("button", { name: `About ${t}` })).toBeInTheDocument();
    expect(await screen.findByRole("img", { name: /^CPU, current 8%, min 6%, max 10%/ })).toBeInTheDocument();
    expect(screen.getByText("steady at 31 all window")).toBeInTheDocument();
    expect(screen.getByText("limit 65,536")).toBeInTheDocument();
  });

  it("draws the CPU chart neutral with the accent only on its changes, like the Overview", async () => {
    show(ap3(), "nats-ap-3");
    const img = (await screen.findAllByRole("img", { name: /^CPU, current 8%/ })).find((e) => e.matches("svg.spark"))!;
    const line = img.querySelector("polyline:not(.spark-change)") as SVGElement;
    expect(line.style.stroke).toBe("var(--text-muted)");
    expect((img.querySelector(".spark-change, .spark-now") as SVGElement).style.stroke).toBe("var(--accent)");
  });

  it("charts JetStream file bytes and shows the current storage with its own configured limit", async () => {
    const r = ap3();
    r.server!.jsStorageBytes = 1n << 20n;
    r.server!.jsMaxStorageBytes = 2n << 20n;
    m.getTimeSeries.mockImplementation(async (req) => create(GetTimeSeriesResponseSchema, {
      series: { metric: req.metric, unit: Unit.BYTES, points: req.metric === "server.js_storage_bytes" ? points([0, 1 << 20]) : [], min: 0, max: 1 << 20 },
    }));
    show(r, "nats-ap-3");
    const about = await screen.findByRole("button", { name: "About JetStream file storage" });
    const tile = about.closest(".mbox")!;
    expect(tile).toHaveTextContent("1 MiB / 2 MiB");
    expect(tile).toHaveTextContent("50% of configured limit");
    expect(tile).toHaveTextContent("Filesystem used and capacity are not collected");
    expect(await within(tile as HTMLElement).findByRole("img", { name: /^JetStream file storage, current 1 MiB, min 0 B, max 1 MiB/ })).toBeInTheDocument();
    expect(m.getTimeSeries.mock.calls.filter(([req]) => req.metric === "server.js_storage_bytes")).toHaveLength(1);
    const req = m.getTimeSeries.mock.calls.find(([req]) => req.metric === "server.js_storage_bytes")![0];
    expect(req.entity).toMatchObject({ name: "nats-ap-3", server: "nats-ap-3" });
  });

  it.each([
    ["unknown limit", true, 0n, undefined, "0 B · limit unknown"],
    ["missing bytes", true, undefined, 2n << 20n, "not reported · limit 2 MiB"],
    ["down", false, 1n << 20n, 2n << 20n, "did not answer"],
  ] as const)("keeps storage %s distinct from zero", async (_case, present, used, limit, expected) => {
    const r = ap3();
    r.server!.present = present;
    r.server!.jsStorageBytes = used;
    r.server!.jsMaxStorageBytes = limit;
    show(r, "nats-ap-3");
    const about = await screen.findByRole("button", { name: "About JetStream file storage" });
    const tile = about.closest(".mbox")!;
    await waitFor(() => expect(tile.querySelector(".mb-v")).not.toHaveTextContent("…"));
    expect(tile.querySelector(".mb-v")).toHaveTextContent(present && used !== undefined ? "0 B" : "–");
    expect(tile).toHaveTextContent(expected);
    expect(tile).not.toHaveTextContent("% of configured limit");
  });

  it("labels a server without JetStream without presenting stored zero as current usage", async () => {
    const r = ap3();
    r.server!.role = ServerRole.NO_JETSTREAM;
    r.server!.jsStorageBytes = 0n;
    show(r, "nats-ap-3");
    const about = await screen.findByRole("button", { name: "About JetStream file storage" });
    const tile = about.closest(".mbox")!;
    await waitFor(() => expect(tile.querySelector(".mb-v")).not.toHaveTextContent("…"));
    expect(tile.querySelector(".mb-v")).toHaveTextContent("–");
    expect(tile).toHaveTextContent("no JetStream");
    expect(tile).not.toHaveTextContent("% of configured limit");
  });

  it("names server slow-consumer detections separately from JetStream backlog", async () => {
    show(ap3(), "nats-ap-3");
    const about = await screen.findByRole("button", { name: "About Slow-consumer detections" });
    const tile = about.closest(".mbox")! as HTMLElement;
    expect(within(tile).queryByRole("link", { name: "Consumers" })).not.toBeInTheDocument();
    expect(within(tile).getByRole("link", { name: "Investigate detections" })).toHaveAttribute("href", expect.stringContaining("/servers/slow-consumers?"));
    expect(tile).toHaveTextContent("NATS had trouble sending data because a receiving connection could not keep up.");
    await userEvent.click(about);
    expect(screen.getByRole("tooltip")).toHaveTextContent("Server slow-consumer counter increases during the selected range.");
    expect(screen.getByRole("tooltip")).toHaveTextContent("Covers clients, routes, gateways and leaf connections; retries can count without a disconnect.");
    expect(screen.getByRole("tooltip")).toHaveTextContent("Not unique clients or JetStream pending messages.");
    expect(tile).not.toHaveTextContent("Clients disconnected as slow");
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

  it("reads the three information cards before a separate recent-events row", async () => {
    show(ap3(), "nats-ap-3");
    const where = await screen.findByRole("region", { name: "Where it runs" });
    const routes = screen.getByRole("region", { name: "Routes" });
    const limits = screen.getByRole("region", { name: "Limits and settings" });
    const recent = screen.getByRole("region", { name: "Recent events" });
    const cards = where.parentElement!;
    expect(Array.from(cards.children)).toEqual([where, routes, limits]);
    expect(cards.nextElementSibling).toContainElement(recent);
    expect(within(recent).getByRole("heading", { level: 2, name: "Recent events" })).toBeInTheDocument();
    expect(await within(recent).findByRole("link", { name: "label-printer reconnected" })).toHaveAttribute("href", "/events?id=2");
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
    expect(h.parentElement).toHaveTextContent(/did not answer in the newest collection$/);
    const cpu = screen.getByRole("button", { name: "About CPU" }).closest(".mbox")!;
    await waitFor(() => expect(cpu.querySelector(".mb-v b")).toHaveTextContent("–"));
    expect(cpu).not.toHaveTextContent("5%");
    expect(cpu.querySelector(".mb-dot")).toBeNull();
    expect(cpu.querySelector(".mb-range")).not.toBeNull();
    expect(cpu).toHaveTextContent("no current value");
    expect(cpu).not.toHaveTextContent("no data");
  });
});
