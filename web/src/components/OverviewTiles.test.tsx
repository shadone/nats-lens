import { renderApp as render } from "../test/render";
import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Tile } from "../api/model";
import { OverviewTiles, tileStatus, tileText } from "./OverviewTiles";

const START = Date.parse("2026-10-16T14:30:00Z");
const END = START + 86_400_000;

function tile(extra: Partial<Tile>): Tile {
  return { id: "servers", label: "Servers", unit: "count", valueSuffix: "", value2Suffix: "", subtitle: "", status: "ok", description: "Servers that answered.", ...extra };
}

const series = (vals: number[], min: number, max: number) => ({
  metric: "cluster.servers_up",
  unit: "count" as const,
  stepMs: 900_000,
  points: vals.map((v, i) => ({ time: START + i * 900_000, value: v })),
  min,
  max,
});

describe("OverviewTiles", () => {
  it("shows a tile without a value as a dash with no dot and no zero", () => {
    const { container } = render(<OverviewTiles tiles={[tile({ value: undefined, series: series([2, 3], 2, 3) })]} windows={[]} start={START} end={END} />);
    expect(screen.getByText("–")).toBeInTheDocument();
    expect(screen.queryByText("0")).toBeNull();
    expect(container.querySelector(".mb-dot")).toBeNull();
    expect(screen.getByText("no current value")).toBeInTheDocument();
  });

  it("puts the dot at 50% for the midpoint of the window", () => {
    const { container } = render(<OverviewTiles tiles={[tile({ value: 15, series: series([10, 20, 15], 10, 20) })]} windows={[]} start={START} end={END} />);
    expect(container.querySelector<HTMLElement>(".mb-dot")!.style.left).toBe("50%");
  });

  it("says steady at 3 all window when min equals max", () => {
    render(<OverviewTiles tiles={[tile({ value: 3, valueSuffix: "/ 3", series: series([3, 3], 3, 3) })]} windows={[]} start={START} end={END} />);
    expect(screen.getByText("steady at 3 all window")).toBeInTheDocument();
    expect(screen.getByText("3 / 3")).toBeInTheDocument();
  });

  it("names the meta leader, and puts a limit in the note rather than the value", () => {
    render(
      <OverviewTiles
        tiles={[
          tile({ id: "meta_leader", label: "Meta leader", value: 0, valueText: "nats-1", subtitle: "0 leader changes in range" }),
          tile({ id: "storage", label: "Storage", unit: "bytes", value: 768 * 1048576, valueSuffix: "of 150 GiB", limit: 150 * 1073741824, subtitle: "all replicas · 0.5% used" }),
        ]}
        windows={[]}
        start={START}
        end={END}
      />,
    );
    expect(screen.getByText("nats-1")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "About Meta leader" }).closest(".mbox")!.querySelector(".mb-range, .mb-track-note")).toBeNull();
    expect(screen.getByText("768 MiB")).toBeInTheDocument();
    expect(screen.getByText("all replicas · 0.5% used · limit 150 GiB")).toBeInTheDocument();
  });

  it("shows the description behind the (i) button", () => {
    render(<OverviewTiles tiles={[tile({ value: 3 })]} windows={[]} start={START} end={END} />);
    expect(screen.getByRole("button", { name: "About Servers" })).toBeInTheDocument();
  });
});

describe("tileText", () => {
  it("joins a second value", () => {
    expect(tileText(tile({ value: 61, value2: 63, valueSuffix: "in", value2Suffix: "out" }))).toBe("61 in, 63 out");
  });
});

describe("tileStatus", () => {
  it("reads a tile with a value and no check behind it as ok", () => {
    expect(tileStatus(tile({ id: "accounts", value: 3, status: "nodata" }))).toBe("ok");
    expect(tileStatus(tile({ id: "accounts", value: undefined, status: "nodata" }))).toBe("nodata");
    expect(tileStatus(tile({ id: "streams", value: 3, status: "warning" }))).toBe("warning");
  });
});

it("identifies the headline's measurement instead of calling every tile a snapshot", () => {
  render(<OverviewTiles tiles={[tile({ value: 3 }), tile({ id: "meta_leader", label: "Meta leader", valueText: "nats-1" }), tile({ id: "storage", label: "Storage", unit: "bytes", value: 40 })]} windows={[]} start={START} end={END} />);
  expect(screen.getByText("Latest count bucket")).toBeInTheDocument();
  expect(screen.getByText("Newest leader · chart: changes per bucket")).toBeInTheDocument();
  expect(screen.getByText("Newest storage reports")).toBeInTheDocument();
});

it("never presents a leader-change total as the meta leader when no majority names one", () => {
  const { container } = render(<OverviewTiles tiles={[tile({ id: "meta_leader", label: "Meta leader", value: 2, subtitle: "2 leader changes in range", series: series([0, 2], 0, 2) })]} windows={[]} start={START} end={END} />);
  expect(screen.getByText("Unknown")).toBeInTheDocument();
  expect(screen.getByText("2 leader changes in range")).toBeInTheDocument();
  expect(container.querySelector(".mb-range")).toBeNull();
});
