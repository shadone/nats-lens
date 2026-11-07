import { create, fromJson } from "@bufbuild/protobuf";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { lens } from "../api/client";
import { Status } from "../gen/natslens/v1/common_pb";
import { ListServersResponseSchema, type ListServersResponse } from "../gen/natslens/v1/servers_pb";
import { GetTimeSeriesBatchResponseSchema, GetTimeSeriesResponseSchema } from "../gen/natslens/v1/timeseries_pb";
import { exampleGlobalCpu, exampleGlobalServers } from "../test/fixtures/exampleGlobal";
import { renderApp } from "../test/render";
import { Servers } from "./Servers";

vi.mock("../api/client", () => ({ lens: { listServers: vi.fn(), getTimeSeries: vi.fn(), getTimeSeriesBatch: vi.fn() } }));
const listServers = vi.mocked(lens.listServers);
const getTimeSeries = vi.mocked(lens.getTimeSeries);
const getTimeSeriesBatch = vi.mocked(lens.getTimeSeriesBatch);

const global = () => fromJson(ListServersResponseSchema, exampleGlobalServers);

function serve(r: ListServersResponse) {
  listServers.mockResolvedValue(r);
}

beforeEach(() => {
  vi.resetAllMocks();
  getTimeSeriesBatch.mockImplementation(async (req) =>
    create(GetTimeSeriesBatchResponseSchema, {
      results: (req.queries ?? []).map((q) => ({ series: q.metric === "cluster.cpu_percent" ? fromJson(GetTimeSeriesResponseSchema, exampleGlobalCpu).series : undefined })),
    }),
  );
});

const list = () => screen.getByRole("region", { name: "Server list" });
const groupHeads = () => within(list()).queryAllByRole("rowheader");

async function showAll() {
  await userEvent.click(within(list()).getByRole("button", { name: /^All/ }));
}

describe("Servers on example-global", () => {
  it("groups the list into three clusters", async () => {
    serve(global());
    renderApp(<Servers />, "/servers");
    await screen.findByRole("region", { name: "Server list" });
    await showAll();
    expect(groupHeads().map((h) => h.querySelector(".grp-name")?.textContent)).toEqual(["us", "ap", "eu"]);
  });

  it("puts the down server first in its group", async () => {
    serve(global());
    renderApp(<Servers />, "/servers");
    await screen.findByRole("region", { name: "Server list" });
    await showAll();
    const rows = within(list()).getAllByRole("row");
    const us = rows.findIndex((r) => r.querySelector(".grp-name")?.textContent === "us");
    expect(rows[us + 1]).toHaveTextContent("nats-us-4");
    expect(rows[us + 1]).toHaveTextContent("did not answer");
  });

  it("lists the servers that need attention by default, every server under All", async () => {
    serve(global());
    renderApp(<Servers />, "/servers");
    await screen.findByRole("region", { name: "Server list" });
    expect(within(list()).getByRole("button", { name: "Needs attention 5" })).toHaveAttribute("aria-pressed", "true");
    const names = within(list())
      .getAllByRole("link")
      .map((a) => a.textContent);
    expect(names.sort()).toEqual(["nats-ap-2", "nats-ap-4", "nats-ap-5", "nats-us-3", "nats-us-4"]);
    await showAll();
    expect(within(list()).getByRole("button", { name: "All 24" })).toHaveAttribute("aria-pressed", "true");
    // Six per cluster, then "Show N more".
    expect(within(list()).getByRole("button", { name: "Show 3 more in eu" })).toBeInTheDocument();
    await userEvent.click(within(list()).getByRole("button", { name: "Show 3 more in eu" }));
    await userEvent.click(within(list()).getByRole("button", { name: "Show 3 more in us" }));
    expect(within(list()).getAllByRole("link")).toHaveLength(24);
  });

  it("shows an info finding in the State column without counting it as a reason", async () => {
    const r = global();
    const eu0 = r.servers.find((s) => s.name === "nats-eu-0")!;
    eu0.status = Status.INFO;
    serve(r);
    renderApp(<Servers />, "/servers");
    await screen.findByRole("region", { name: "Server list" });
    expect(within(list()).queryByRole("link", { name: "nats-eu-0" })).toBeNull();
    await showAll();
    const row = within(list()).getByRole("link", { name: "nats-eu-0" }).closest("tr")!;
    expect(row).toHaveTextContent("ok · info finding");
    expect(row).not.toHaveClass("row-warn");
  });

  it("filters by name, version or tag", async () => {
    serve(global());
    renderApp(<Servers />, "/servers");
    await screen.findByRole("region", { name: "Server list" });
    await showAll();
    await userEvent.type(within(list()).getByRole("searchbox", { name: "Filter servers" }), "2.12.2");
    expect(within(list()).getAllByRole("link").map((a) => a.textContent)).toEqual(["nats-ap-4", "nats-ap-5"]);
    await userEvent.clear(within(list()).getByRole("searchbox", { name: "Filter servers" }));
    await userEvent.type(within(list()).getByRole("searchbox", { name: "Filter servers" }), "az:eu-1b");
    expect(within(list()).getAllByRole("link").map((a) => a.textContent)).toEqual(["nats-eu-1", "nats-eu-4", "nats-eu-7"]);
  });

  it("colours by store and marks the 71% server in the top band", async () => {
    serve(global());
    renderApp(<Servers />, "/servers");
    const map = await screen.findByRole("region", { name: "Every server" });
    await userEvent.click(within(map).getByRole("button", { name: "Store used" }));
    const cell = within(map).getByRole("link", { name: "nats-ap-2: Store used 71%" });
    expect(cell).toHaveClass("heat-4");
    expect(within(map).getByRole("link", { name: "nats-eu-0: Store used 35%" })).toHaveClass("heat-3");
  });

  it("outlines the meta leader, dashes the down server, and draws each legend swatch", async () => {
    serve(global());
    renderApp(<Servers />, "/servers");
    const map = await screen.findByRole("region", { name: "Every server" });
    expect(within(map).getByRole("link", { name: "nats-eu-1: CPU 4%, meta leader" })).toHaveClass("leader");
    expect(within(map).getByRole("link", { name: "nats-us-4: did not answer" })).toHaveClass("down");
    const legend = within(map).getByRole("list", { name: "Colours for CPU" });
    for (const li of within(legend).getAllByRole("listitem")) expect(li.querySelector(".cell-sw")).not.toBeNull();
    expect(legend).toHaveTextContent("60% or more");
  });

  it("shows the data points: answering, versions, store and slow consumers", async () => {
    serve(global());
    renderApp(<Servers />, "/servers");
    await screen.findByRole("region", { name: "Every server" });
    expect(screen.getByText("of 24")).toBeInTheDocument();
    expect(screen.getByText("nats-us-4 did not answer", { exact: false })).toBeInTheDocument();
    expect(screen.getByRole("img", { name: "1 did not answer: nats-us-4" })).toBeInTheDocument();
    expect(screen.getByText("fullest", { exact: false })).toHaveTextContent("range 27 to 71% · fullest nats-ap-2");
    expect(screen.getByText("of them on", { exact: false })).toHaveTextContent("11 of them on nats-us-3");
    expect(screen.getByText("24 servers in 3 clusters · meta leader nats-eu-1")).toBeInTheDocument();
  });

  it("distinguishes slow-consumer detections from JetStream backlog and keeps global scope on its Consumers link", async () => {
    const r = global();
    for (const server of r.servers) server.slowConsumersInRange = 0n;
    serve(r);
    renderApp(<Servers />, "/servers?range=1h&system=example-global&account=SHOP");
    const title = await screen.findByText("Slow-consumer detections, 1 h");
    const card = title.closest(".dp")! as HTMLElement;
    expect(card.querySelector(".dp-v")).toHaveTextContent("0");
    expect(card).toHaveTextContent("Server slow-consumer counter increases during the selected range.");
    expect(within(card).getByRole("img", { name: /^Slow-consumer detections per range, peak none$/ })).toBeInTheDocument();
    expect(card).toHaveTextContent("Covers clients, routes, gateways and leaf connections; retries can count without a disconnect.");
    expect(card).toHaveTextContent("Not unique clients or JetStream pending messages.");
    expect(card).not.toHaveTextContent("Clients disconnected as slow");
    const target = new URL(within(card).getByRole("link", { name: "Consumers" }).getAttribute("href")!, "http://example.com");
    expect(target.pathname).toBe("/consumers");
    expect(Object.fromEntries(target.searchParams)).toEqual({ range: "1h", system: "example-global", account: "SHOP" });
  });

  it("preserves aggregate detections without inferring their connection type", async () => {
    // The API only supplies the aggregate: neither source nor unique clients can be inferred.
    const r = global();
    for (const server of r.servers) server.slowConsumersInRange = 0n;
    r.servers[0].slowConsumersInRange = 7n;
    serve(r);
    renderApp(<Servers />, "/servers");
    const title = await screen.findByText("Slow-consumer detections, 24 h");
    const card = title.closest(".dp")! as HTMLElement;
    expect(card.querySelector(".dp-v")).toHaveTextContent("7");
    expect(card).toHaveTextContent("retries can count without a disconnect");
  });

  it("names the busiest server in the CPU chart's legend", async () => {
    serve(global());
    renderApp(<Servers />, "/servers");
    expect(await screen.findByText(/^busiest: nats-ap-2, [\d.]+%$/)).toBeInTheDocument();
  });

  it("reads the fleet charts and the slow-consumer strip in one batch", async () => {
    serve(global());
    renderApp(<Servers />, "/servers");
    await screen.findByText(/^busiest: nats-ap-2/);
    expect(getTimeSeriesBatch).toHaveBeenCalledTimes(1);
    const queries = getTimeSeriesBatch.mock.calls[0][0].queries ?? [];
    expect(queries.map((q) => [q.metric, q.maxPoints ?? 0])).toEqual([
      ["cluster.connections", 0],
      ["cluster.cpu_percent", 0],
      ["cluster.in_msgs_rate", 0],
      ["cluster.mem_bytes", 0],
      ["cluster.out_msgs_rate", 0],
      ["cluster.slow_consumers", 24],
    ]);
    expect(getTimeSeries).not.toHaveBeenCalled();
  });

  it("shows a series' own error on its chart only", async () => {
    getTimeSeriesBatch.mockImplementation(async (req) =>
      create(GetTimeSeriesBatchResponseSchema, {
        results: (req.queries ?? []).map((q) =>
          q.metric === "cluster.connections" ? { error: "not found: no such series" } : { series: q.metric === "cluster.cpu_percent" ? fromJson(GetTimeSeriesResponseSchema, exampleGlobalCpu).series : undefined },
        ),
      }),
    );
    serve(global());
    renderApp(<Servers />, "/servers");
    const charts = await screen.findByRole("region", { name: "All servers" });
    expect(await within(charts).findByText(/no such series/)).toBeInTheDocument();
    expect(within(charts).getAllByText(/no such series/)).toHaveLength(1);
    expect(await screen.findByText(/^busiest: nats-ap-2/)).toBeInTheDocument();
  });

  it("draws its charts with the Overview's trend charts, whose y axis starts at zero", async () => {
    serve(global());
    renderApp(<Servers />, "/servers");
    const charts = await screen.findByRole("region", { name: "All servers" });
    expect([...charts.querySelectorAll(".tc .lbl")].map((e) => e.textContent)).toEqual(["CPU", "Memory", "Connections", "Messages"]);
    expect(charts.querySelector(".sc-svg, .sc-legend")).toBeNull();
    expect(within(charts).getByRole("list", { name: "CPU legend" })).toBeInTheDocument();
  });
});

describe("Servers, failing cases", () => {
  it("keeps the cluster heading when a filter leaves one of three clusters, in both views", async () => {
    serve(global());
    renderApp(<Servers />, "/servers");
    await screen.findByRole("region", { name: "Server list" });
    await userEvent.type(within(list()).getByRole("searchbox", { name: "Filter servers" }), "nats-ap");
    expect(groupHeads().map((h) => h.querySelector(".grp-name")?.textContent)).toEqual(["ap"]);
    expect(list()).not.toHaveTextContent("no cluster");
    await showAll();
    expect(groupHeads().map((h) => h.querySelector(".grp-name")?.textContent)).toEqual(["ap"]);
  });

  it("keeps the store band inside its track when every store is full", async () => {
    const r = global();
    for (const s of r.servers) s.jsStorageBytes = s.jsMaxStorageBytes;
    serve(r);
    renderApp(<Servers />, "/servers");
    const track = await screen.findByRole("img", { name: /^Store used: median 100%/ });
    const band = track.querySelector(".band") as HTMLElement;
    expect(parseFloat(band.style.left) + parseFloat(band.style.width)).toBeLessThanOrEqual(100);
  });

  it("names the CPU chart's median, its period and its range", async () => {
    serve(global());
    renderApp(<Servers />, "/servers");
    expect(await screen.findByRole("img", { name: /^CPU, current [\d.]+% median, .* UTC, min [\d.]+% · max [\d.]+%$/ })).toBeInTheDocument();
  });

  it("shows a dash for a down server's leaders", async () => {
    serve(global());
    renderApp(<Servers />, "/servers");
    await screen.findByRole("region", { name: "Server list" });
    const row = within(list()).getByRole("link", { name: "nats-us-4" }).closest("tr")!;
    const leadersCell = row.querySelectorAll("td")[8];
    expect(leadersCell).toHaveTextContent("–");
    expect(leadersCell).not.toHaveTextContent("0");
  });

  it("shows one cluster as a flat list", async () => {
    const r = global();
    r.servers = r.servers.filter((s) => s.cluster === "eu");
    serve(r);
    renderApp(<Servers />, "/servers");
    await screen.findByRole("region", { name: "Server list" });
    await showAll();
    expect(groupHeads()).toHaveLength(0);
    expect(within(list()).getAllByRole("link")).toHaveLength(9);
  });

  it("shows a dash and no colour for a server without cpu_percent, never 0%", async () => {
    const r = global();
    r.servers = r.servers.filter((s) => s.cluster === "eu");
    r.servers[0].cpuPercent = undefined;
    serve(r);
    renderApp(<Servers />, "/servers");
    const map = await screen.findByRole("region", { name: "Every server" });
    const cell = within(map).getByRole("link", { name: "nats-eu-0: CPU –" });
    expect(cell).toHaveClass("none");
    expect(cell.className).not.toMatch(/heat-/);
    await showAll();
    const row = within(list()).getByRole("link", { name: "nats-eu-0" }).closest("tr")!;
    const cpu = row.querySelectorAll("td")[5];
    expect(cpu).toHaveTextContent("–");
    expect(cpu).not.toHaveTextContent("0%");
  });
});
