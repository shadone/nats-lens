import { screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { A, B, rgone } from "../test/fixtures/replay";
import { renderApp } from "../test/render";
import { ReplayGone } from "./ReplayGone";

vi.mock("../api/requests", () => ({ getLensInfo: vi.fn(() => new Promise(() => {})), getShellStatus: vi.fn(() => new Promise(() => {})) }));

const deleted = rgone({
  entity: { kind: "consumer", key: "APP/order-archive/analytics-aggregator", name: "analytics-aggregator", account: "APP", stream: "order-archive", consumer: "analytics-aggregator", server: "" },
  deleteEvent: { id: "41", kind: "consumer.deleted", summary: "analytics-aggregator on order-archive by analytics-worker@10.20.0.12" },
  deleteClient: "analytics-worker (10.20.0.12)",
});

describe("ReplayGone", () => {
  it("names the deleting client once, in the event's summary", () => {
    renderApp(<ReplayGone gone={[deleted]} truncated={false} moments={{ a: A, b: B }} read />);
    const row = screen.getByRole("link", { name: "analytics-aggregator" }).closest("tr")!;
    expect(within(row).getAllByText(/analytics-worker/)).toHaveLength(1);
    expect(within(row).getByRole("link", { name: "analytics-aggregator on order-archive by analytics-worker@10.20.0.12" })).toHaveAttribute("href", "/events?id=41");
  });

  it("names the client beside an event without a summary", () => {
    renderApp(<ReplayGone gone={[{ ...deleted, deleteEvent: { ...deleted.deleteEvent!, summary: "" } }]} truncated={false} moments={{ a: A, b: B }} read />);
    expect(screen.getByText("by analytics-worker (10.20.0.12)")).toBeInTheDocument();
  });

  it("shows a dash for a consumer's durables, under a short header", () => {
    renderApp(<ReplayGone gone={[deleted]} truncated={false} moments={{ a: A, b: B }} read />);
    expect(screen.getAllByRole("columnheader").at(-1)).toHaveTextContent(/^Durables$/);
    const cells = screen.getByRole("link", { name: "analytics-aggregator" }).closest("tr")!.querySelectorAll("td");
    expect(cells[cells.length - 1]).toHaveTextContent("—not a stream");
  });

  it("says in a user's words when the gone assets were not observed, without a link to compare", () => {
    renderApp(<ReplayGone gone={[]} truncated={false} moments={{ a: A, b: B }} read={false} />);
    expect(screen.getByText('Not observed for this A and B; "Between A and B" says why.')).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Compare in Placement" })).toBeNull();
  });

  it("offers Compare in Placement when the gone assets were read", () => {
    renderApp(<ReplayGone gone={[]} truncated={false} moments={{ a: A, b: B }} read />);
    expect(screen.getByText("No stream or durable consumer that servers held at A is missing at B.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Compare in Placement" })).toBeInTheDocument();
  });
});
