import { fireEvent, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { renderApp } from "../test/render";
import { RangeNotice, RangePicker, fromLocalInput, toLocalInput } from "./RangePicker";

const loc = () => screen.getByTestId("location").textContent;

describe("RangePicker URL state", () => {
  it("writes a chosen preset into the URL and keeps other parameters", async () => {
    renderApp(<RangePicker />, "/events?src=nats");
    await userEvent.click(screen.getByRole("button", { name: "Last 24h" }));
    await userEvent.click(screen.getByRole("button", { name: "6h" }));
    expect(loc()).toBe("/events?src=nats&range=6h");
    expect(screen.getByRole("button", { name: "Last 6h" })).toBeInTheDocument();
  });

  it("reads the range from the URL", () => {
    renderApp(<RangePicker />, "/?from=2026-10-16T09:00:00Z&to=2026-10-16T10:00:00Z");
    expect(screen.getByRole("button", { name: "Custom" })).toBeInTheDocument();
    expect(screen.getByText(/16 Oct 09:00 – 16 Oct 10:00/)).toBeInTheDocument();
  });

  it("refuses a custom range that ends before it starts", async () => {
    renderApp(<RangePicker />, "/");
    await userEvent.click(screen.getByRole("button", { name: "Last 24h" }));
    fireEvent.change(screen.getByLabelText("From"), { target: { value: "2026-10-16T10:00" } });
    fireEvent.change(screen.getByLabelText("To"), { target: { value: "2026-10-16T09:00" } });
    await userEvent.click(screen.getByRole("button", { name: "Apply" }));
    expect(screen.getByRole("alert")).toHaveTextContent("before the end");
    expect(loc()).toBe("/");
  });

  it("applies a custom range as UTC", async () => {
    renderApp(<RangePicker />, "/streams");
    await userEvent.click(screen.getByRole("button", { name: "Last 24h" }));
    fireEvent.change(screen.getByLabelText("From"), { target: { value: "2026-10-16T09:00" } });
    fireEvent.change(screen.getByLabelText("To"), { target: { value: "2026-10-16T10:30" } });
    await userEvent.click(screen.getByRole("button", { name: "Apply" }));
    expect(loc()).toBe("/streams?from=2026-10-16T09%3A00%3A00Z&to=2026-10-16T10%3A30%3A00Z");
  });

  it("pauses live mode into a fixed range and goes live again", async () => {
    renderApp(<RangePicker />, "/?range=1h");
    await userEvent.click(screen.getByRole("button", { name: /Live: pause/ }));
    expect(loc()).toMatch(/^\/\?from=.*&to=/);
    await userEvent.click(screen.getByRole("button", { name: /go live/ }));
    expect(loc()).toBe("/?range=1h");
  });

  it("converts datetime-local values as UTC", () => {
    expect(fromLocalInput("2026-10-16T09:00")).toBe(Date.parse("2026-10-16T09:00:00Z"));
    expect(toLocalInput(Date.parse("2026-10-16T09:00:00Z"))).toBe("2026-10-16T09:00");
    expect(fromLocalInput("")).toBeNaN();
  });
});

describe("RangePicker history", () => {
  const nav = () => screen.getByTestId("navigation").textContent;

  it("replaces the history entry for a preset and for pause", async () => {
    renderApp(<RangePicker />, "/");
    await userEvent.click(screen.getByRole("button", { name: "Last 24h" }));
    await userEvent.click(screen.getByRole("button", { name: "6h" }));
    expect(nav()).toBe("REPLACE");
    await userEvent.click(screen.getByRole("button", { name: /Live: pause/ }));
    expect(nav()).toBe("REPLACE");
  });

  it("pushes a history entry for a custom range", async () => {
    renderApp(<RangePicker />, "/");
    await userEvent.click(screen.getByRole("button", { name: "Last 24h" }));
    fireEvent.change(screen.getByLabelText("From"), { target: { value: "2026-10-16T09:00" } });
    fireEvent.change(screen.getByLabelText("To"), { target: { value: "2026-10-16T10:30" } });
    await userEvent.click(screen.getByRole("button", { name: "Apply" }));
    expect(nav()).toBe("PUSH");
  });

  it("refuses a custom range in the future", async () => {
    renderApp(<RangePicker />, "/");
    await userEvent.click(screen.getByRole("button", { name: "Last 24h" }));
    fireEvent.change(screen.getByLabelText("From"), { target: { value: "2099-01-01T09:00" } });
    fireEvent.change(screen.getByLabelText("To"), { target: { value: "2099-01-01T10:00" } });
    await userEvent.click(screen.getByRole("button", { name: "Apply" }));
    expect(screen.getByRole("alert")).toHaveTextContent("future");
    expect(loc()).toBe("/");
  });
});

describe("RangeNotice", () => {
  it("says the link's range was not used and can clear it", async () => {
    renderApp(<RangeNotice />, "/streams?src=nats&from=1&to=2");
    expect(screen.getByRole("status")).toHaveTextContent(/not valid times.*showing the last 24 hours/);
    await userEvent.click(screen.getByRole("button", { name: "Remove it from the link" }));
    expect(loc()).toBe("/streams?src=nats");
    expect(screen.queryByRole("status")).toBeNull();
  });
});

describe("RangePicker panel focus", () => {
  it("moves focus in, keeps it inside, and returns it on Escape", async () => {
    renderApp(<RangePicker />, "/");
    const trigger = screen.getByRole("button", { name: "Last 24h" });
    await userEvent.click(trigger);
    expect(screen.getByRole("button", { name: "1h" })).toHaveFocus();
    await userEvent.tab({ shift: true });
    expect(screen.getByRole("button", { name: "Apply" })).toHaveFocus();
    await userEvent.tab();
    expect(screen.getByRole("button", { name: "1h" })).toHaveFocus();
    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("dialog", { name: "Time range" })).toBeNull();
    expect(trigger).toHaveFocus();
  });
});

describe("RangePicker panel after a click on a gap", () => {
  it("keeps focus in the panel, so Tab and Escape still work", async () => {
    renderApp(<RangePicker />, "/");
    const trigger = screen.getByRole("button", { name: "Last 24h" });
    await userEvent.click(trigger);
    const panel = screen.getByRole("dialog", { name: "Time range" });
    await userEvent.click(screen.getByText("Custom, UTC"));
    expect(panel).toHaveFocus();
    await userEvent.tab();
    expect(screen.getByRole("button", { name: "1h" })).toHaveFocus();
    await userEvent.click(screen.getByText("Custom, UTC"));
    await userEvent.tab({ shift: true });
    expect(screen.getByRole("button", { name: "Apply" })).toHaveFocus();
    await userEvent.click(screen.getByText("Custom, UTC"));
    await userEvent.keyboard("{Escape}");
    expect(trigger).toHaveFocus();
  });
});
