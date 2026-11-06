import { fireEvent, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { renderApp } from "../test/render";
import { RecentEvents } from "./ServerDetailParts";

vi.mock("../api/servers", () => ({
  useServerEvents: () => ({ data: [{
    id: "synthetic-selection", summary: "Synthetic summary", kind: "consumer.deleted", source: "nats", repeats: 1,
  }] }),
}));

function setup() {
  renderApp(<RecentEvents name="server-1" spec={{ kind: "live", preset: "6h" }} end={0} />, "/servers/server-1?range=6h&system=shop");
  const link = screen.getByRole("link", { name: "Synthetic summary" });
  const range = document.createRange();
  range.selectNodeContents(link);
  window.getSelection()!.removeAllRanges();
  window.getSelection()!.addRange(range);
  return link;
}

describe("recent event selection", () => {
  it("keeps the server page while selecting, then follows an ordinary click", () => {
    const link = setup();
    expect(link).toHaveAttribute("draggable", "false");
    fireEvent.click(link, { detail: 1 });
    expect(screen.getByTestId("location")).toHaveTextContent("/servers/server-1?range=6h&system=shop");
    window.getSelection()!.removeAllRanges();
    fireEvent.click(link, { detail: 1 });
    expect(screen.getByTestId("location").textContent).toContain("/events?id=synthetic-selection");
    expect(screen.getByTestId("location").textContent).toContain("range=6h");
    expect(screen.getByTestId("location").textContent).toContain("system=shop");
  });

  it("follows keyboard activation despite a selection in the link", () => {
    const link = setup();
    fireEvent.click(link, { detail: 0 });
    expect(screen.getByTestId("location").textContent).toContain("/events?id=synthetic-selection");
  });
});
