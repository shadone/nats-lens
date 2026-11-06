import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderApp } from "../test/render";
import { Freshness, RangePicker } from "./RangePicker";

afterEach(() => vi.restoreAllMocks());

describe("range header feedback", () => {
  it("keeps preset bounds inside the popover", async () => {
    const { container } = renderApp(<RangePicker />, "/?range=24h");
    expect(container.querySelector(".range .span")).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "Last 24h" }));
    const bounds = within(screen.getByRole("group", { name: "Selected range" }));
    expect(bounds.getAllByText(/UTC/)).toHaveLength(1);
    expect(container.querySelectorAll(".range-panel time")).toHaveLength(2);
  });

  it.each(["en-GB", "nb-NO", "en-US"])("formats popover dates using %s", async (locale) => {
    vi.spyOn(navigator, "language", "get").mockReturnValue(locale);
    vi.spyOn(navigator, "languages", "get").mockReturnValue([locale]);
    renderApp(<RangePicker />, "/?from=2026-10-16T09:00:00Z&to=2026-10-16T10:00:00Z");
    await userEvent.click(screen.getByRole("button", { name: "Custom" }));
    const format = new Intl.DateTimeFormat(locale, { year: "numeric", month: "numeric", day: "numeric", hour: "numeric", minute: "2-digit", timeZone: "UTC", calendar: "gregory" });
    const bounds = within(screen.getByRole("group", { name: "Selected range" }));
    expect(screen.getByRole("textbox", { name: "From" })).toHaveValue(format.format(Date.parse("2026-10-16T09:00:00Z")));
    expect(screen.getByRole("textbox", { name: "To" })).toHaveValue(format.format(Date.parse("2026-10-16T10:00:00Z")));
    expect(bounds.getByText(format.format(Date.parse("2026-10-16T09:00:00Z")))).toBeInTheDocument();
  });

  it("shows stable live text while retaining freshness details on demand", () => {
    renderApp(<Freshness spec={{ kind: "live", preset: "24h" }} freshness={{ epoch: "9", epochAt: Date.now() - 4_000, live: true, partial: false, collecting: true, intervalMs: 30_000 }} />);
    const live = screen.getByText(/^live/);
    expect(live).toHaveTextContent(/^live$/);
    expect(live).toHaveAttribute("title", expect.stringContaining("4s"));
  });
});
