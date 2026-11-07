import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Timeline } from "../api/model";
import { StatusTimeline } from "./StatusTimeline";

const START = Date.parse("2026-10-17T09:00:00Z");
const Q = 900_000;

const tl: Timeline = {
  start: START,
  bucketMs: Q,
  bucketCount: 4,
  rows: [
    { key: "consumer-010", checkCode: "consumer-010", checkName: "Expected consumer missing", severity: "critical", cells: ["ok", "ok", "critical", "critical"], now: "critical" },
    { key: "server-002", checkCode: "server-002", checkName: "Healthz not ok", severity: "critical", cells: ["ok", "warning", "ok", "nodata"], now: "nodata" },
  ],
  kubernetes: [],
  nats: [],
  windows: [],
  natsTruncated: false, findingTruncated: false, kubernetesTruncated: false,
};

describe("StatusTimeline", () => {
  it("draws one cell per bucket, coloured by level and titled with its time", () => {
    const { container } = render(<StatusTimeline tl={tl} end={START + 4 * Q} live asTable={false} />);
    const svg = screen.getByRole("img", { name: /^consumer-010:/ });
    const rects = svg.querySelectorAll("rect");
    expect(rects).toHaveLength(4);
    expect([...rects].map((r) => r.getAttribute("data-level"))).toEqual(["ok", "ok", "critical", "critical"]);
    expect(rects[2].getAttribute("style")).toContain("--cell-critical");
    expect(rects[2].querySelector("title")?.textContent).toBe("09:30 critical");
    expect(container.querySelectorAll(".crow")).toHaveLength(2);
  });

  it("describes each row's runs in its label", () => {
    render(<StatusTimeline tl={tl} end={START + 4 * Q} live asTable={false} />);
    expect(screen.getByRole("img", { name: "consumer-010: critical 09:30–10:00" })).toBeInTheDocument();
    expect(screen.getByRole("img", { name: "server-002: warning 09:15–09:30, no data 09:45–10:00" })).toBeInTheDocument();
  });

  it("shows the now column and no Kubernetes lane without Kubernetes events", () => {
    const { container } = render(<StatusTimeline tl={tl} end={START + 4 * Q} live asTable={false} />);
    expect(within(container.querySelector(".tl-now")!).getByText("Critical")).toBeInTheDocument();
    expect(container.querySelector(".k8slane")).toBeNull();
  });

  it("names the Kubernetes lane K8s, in the chart and in the table", () => {
    const k8s: Timeline = { ...tl, kubernetes: [{ id: "k1", time: START + Q, source: "kubernetes", kind: "Killing", shortLabel: "Killing", label: "pod nats-0 Killing" }] };
    const { container, unmount } = render(<StatusTimeline tl={k8s} end={START + 4 * Q} live asTable={false} />);
    expect(container.querySelector(".lane .code")).toHaveTextContent(/^K8s$/);
    unmount();
    render(<StatusTimeline tl={k8s} end={START + 4 * Q} live asTable />);
    expect(screen.getByRole("cell", { name: "K8s" })).toBeInTheDocument();
  });

  it("gives the same content as a table", () => {
    render(<StatusTimeline tl={tl} end={START + 4 * Q} live asTable />);
    const rows = screen.getAllByRole("row");
    expect(rows).toHaveLength(3);
    expect(within(rows[1]).getByText("critical 09:30–10:00")).toBeInTheDocument();
  });
});
