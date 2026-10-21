import { fireEvent, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import * as requests from "../api/requests";
import { reviewDateOf, validateAccept, type AcceptForm } from "../lib/muteForm";
import { acceptance } from "../test/fixtures/capacity";
import { renderApp } from "../test/render";
import { AcceptDialog, type AcceptTarget } from "./AcceptDialog";

vi.mock("../api/requests", () => ({ muteFinding: vi.fn(), getLensInfo: vi.fn() }));
const muteFinding = vi.mocked(requests.muteFinding);
const getLensInfo = vi.mocked(requests.getLensInfo);
const info = (identityMode: "iap" | "request") => ({ identityMode, version: "dev", cluster: "c", collectIntervalMs: 30_000, retentionMs: 0, live: true, demo: true });

const target: AcceptTarget = {
  findingId: "stream-005:APP/order-archive",
  checkCode: "stream-005",
  checkName: "Stream without any limit",
  entity: { kind: "stream", key: "APP/order-archive", name: "order-archive", account: "APP", stream: "order-archive", consumer: "", server: "" },
};

const NOW = Date.parse("2026-10-21T14:30:00Z");
const DAY = 86_400_000;
const form: AcceptForm = { reason: "Order history is kept for audits", review: "3mo", reviewDate: "", name: "denis" };

describe("reviewDateOf", () => {
  it("adds calendar months, a year of 364 days, inside the server's 365,, or the end of a chosen UTC day", () => {
    expect(reviewDateOf(form, NOW)).toBe(Date.parse("2027-01-21T14:30:00Z"));
    expect(reviewDateOf({ ...form, review: "6mo" }, NOW)).toBe(Date.parse("2027-04-21T14:30:00Z"));
    expect(reviewDateOf({ ...form, review: "1y" }, NOW)).toBe(NOW + 364 * DAY);
    expect(reviewDateOf({ ...form, review: "date", reviewDate: "2027-02-01" }, NOW)).toBe(Date.parse("2027-02-01T23:59:00Z"));
    expect(reviewDateOf({ ...form, review: "date", reviewDate: "" }, NOW)).toBeNaN();
  });

  it("keeps a month's end inside the shorter month", () => {
    expect(reviewDateOf(form, Date.parse("2026-11-30T10:00:00Z"))).toBe(Date.parse("2027-02-28T10:00:00Z"));
  });
});

describe("validateAccept", () => {
  it("accepts a reason, a name and a preset review date", () => {
    expect(validateAccept(form, NOW, true)).toEqual({});
  });

  it("requires a reason and, when asked, a name", () => {
    expect(Object.keys(validateAccept({ ...form, reason: " ", name: "" }, NOW, true)).sort()).toEqual(["name", "reason"]);
    expect(validateAccept({ ...form, name: "" }, NOW, false)).toEqual({});
    expect(validateAccept({ ...form, reason: "x".repeat(1001) }, NOW, true).reason).toMatch(/1000/);
  });

  it("keeps the review date between a day and 364 days ahead", () => {
    const at = (d: string) => validateAccept({ ...form, review: "date", reviewDate: d }, NOW, true).review;
    expect(at("")).toMatch(/Choose/);
    expect(at("2026-10-21")).toMatch(/at least a day/);
    expect(at("2026-10-22")).toBeUndefined();
    expect(at("2027-10-19")).toBeUndefined();
    expect(at("2027-10-20")).toMatch(/364 days/);
  });
});

describe("AcceptDialog", () => {
  beforeEach(() => {
    localStorage.clear();
    muteFinding.mockReset();
    getLensInfo.mockResolvedValue(info("request"));
  });

  it("names the finding it accepts and sends nothing while the form is invalid", async () => {
    renderApp(<AcceptDialog target={target} onClose={() => {}} />);
    expect(screen.getByText(/^Accepts/)).toHaveTextContent("Accepts stream-005 Stream without any limit for order-archive");
    await userEvent.click(screen.getByRole("button", { name: "Accept as by design" }));
    expect(screen.getByText(/Give a reason/)).toBeInTheDocument();
    expect(screen.getByText(/Enter your name/)).toBeInTheDocument();
    expect(muteFinding).not.toHaveBeenCalled();
  });

  it("accepts the stream's finding until the review date, in 3 months unless chosen", async () => {
    muteFinding.mockResolvedValue(acceptance());
    const onClose = vi.fn();
    const onAccepted = vi.fn();
    renderApp(<AcceptDialog target={target} onClose={onClose} onAccepted={onAccepted} />);
    expect(screen.getByLabelText("Review date")).toHaveValue("3mo");
    expect(screen.getByText(/Between a day and 364 days from now; dates are UTC\./)).toBeInTheDocument();
    await userEvent.type(screen.getByLabelText("Reason"), "Order history is kept for audits");
    await userEvent.type(screen.getByLabelText("Your name"), "denis");
    fireEvent.change(screen.getByLabelText("Review date"), { target: { value: "1y" } });
    const before = Date.now();
    await userEvent.click(screen.getByRole("button", { name: "Accept as by design" }));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    const req = muteFinding.mock.calls[0][1];
    expect(req).toMatchObject({ checkCode: "stream-005", reason: "Order history is kept for audits", mutedBy: "denis", accept: true });
    expect(req.entity?.key).toBe("APP/order-archive");
    expect(req.expiresAt - before).toBeGreaterThanOrEqual(364 * DAY);
    expect(req.expiresAt - before).toBeLessThan(364 * DAY + 60_000);
    expect(onAccepted).toHaveBeenCalledWith("stream-005:APP/order-archive", acceptance());
  });

  it("asks for no name when the server takes identity from IAP", async () => {
    getLensInfo.mockResolvedValue(info("iap"));
    muteFinding.mockResolvedValue(acceptance());
    renderApp(<AcceptDialog target={target} onClose={() => {}} />);
    expect(await screen.findByText(/acceptance is recorded as your IAP identity/)).toBeInTheDocument();
    expect(screen.queryByLabelText("Your name")).toBeNull();
    await userEvent.type(screen.getByLabelText("Reason"), "kept");
    await userEvent.click(screen.getByRole("button", { name: "Accept as by design" }));
    await waitFor(() => expect(muteFinding).toHaveBeenCalled());
    expect(muteFinding.mock.calls[0][1].mutedBy).toBe("");
  });

  it("offers a date between tomorrow and a year ahead", async () => {
    renderApp(<AcceptDialog target={target} onClose={() => {}} />);
    fireEvent.change(screen.getByLabelText("Review date"), { target: { value: "date" } });
    const date = screen.getByLabelText("Review on, UTC");
    expect(date).toHaveAttribute("type", "date");
    expect(Date.parse(`${date.getAttribute("min")}T23:59:00Z`) - Date.now()).toBeGreaterThanOrEqual(DAY);
    expect(Date.parse(`${date.getAttribute("max")}T23:59:00Z`) - Date.now()).toBeLessThanOrEqual(364 * DAY);
  });

  it("shows the server's refusal and stays open", async () => {
    muteFinding.mockRejectedValue(new Error("expires_at, the review date, must be at most 365 days ahead"));
    const onClose = vi.fn();
    renderApp(<AcceptDialog target={target} onClose={onClose} />);
    await userEvent.type(screen.getByLabelText("Reason"), "kept");
    await userEvent.type(screen.getByLabelText("Your name"), "denis");
    await userEvent.click(screen.getByRole("button", { name: "Accept as by design" }));
    expect(await screen.findByText(/365 days ahead/)).toBeInTheDocument();
    expect(onClose).not.toHaveBeenCalled();
  });
});
