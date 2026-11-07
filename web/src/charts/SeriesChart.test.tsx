import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { SeriesChart } from "./SeriesChart";

const series = { metric: "stream.bytes", unit: "bytes" as const, stepMs: 900_000, points: [{ time: 0, value: 1 }, { time: 900_000, value: 2 }], min: 1, max: 2 };

describe("SeriesChart", () => {
  it("keeps drawing the series it has when a refresh fails", () => {
    render(<SeriesChart title="Bytes" series={series} start={0} end={900_000} error="network: Failed to fetch" />);
    expect(screen.getByRole("img", { name: /^Bytes, current 2 B, / })).toBeInTheDocument();
    expect(screen.queryByText(/Could not load/)).toBeNull();
  });

  it("says why when it has nothing to draw", () => {
    render(<SeriesChart title="Bytes" start={0} end={900_000} error="network: Failed to fetch" />);
    expect(screen.getByText("Could not load: network: Failed to fetch")).toBeInTheDocument();
  });
});

describe("SeriesChart after a failed refresh", () => {
  it("marks the old series as stale with the time it is from", () => {
    const at = Date.parse("2026-10-17T14:30:05Z");
    const { container } = render(<SeriesChart title="Bytes" series={series} start={0} end={900_000} error="network: Failed to fetch" updatedAt={at} />);
    expect(container.querySelector(".chart")).toHaveClass("stale");
    expect(screen.getByText("as of 14:30:05 UTC")).toBeInTheDocument();
    expect(screen.getByRole("img", { name: "Bytes, current 2 B, 1 Jan 00:00 to 1 Jan 00:15 UTC, as of 14:30:05 UTC" })).toBeInTheDocument();
  });

  it("is not marked while refreshes succeed", () => {
    const { container } = render(<SeriesChart title="Bytes" series={series} start={0} end={900_000} updatedAt={0} />);
    expect(container.querySelector(".chart")).not.toHaveClass("stale");
    expect(screen.queryByText(/as of/)).toBeNull();
  });
});

describe("SeriesChart colours", () => {
  it("draws the line neutral with the accent only on the change, like the Overview tiles", () => {
    const steps = { ...series, points: [1, 1, 1, 2, 2, 2].map((value, i) => ({ time: i * 900_000, value })) };
    const { container } = render(<SeriesChart title="Bytes" series={steps} start={0} end={5 * 900_000} />);
    expect((container.querySelector("svg.spark polyline:not(.spark-change)") as SVGElement).style.stroke).toBe("var(--text-muted)");
    expect((container.querySelector(".spark-change") as SVGElement).style.stroke).toBe("var(--accent)");
  });
});
