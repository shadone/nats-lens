import { Code, ConnectError } from "@connectrpc/connect";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import * as requests from "../api/requests";
import { reviewDateOf, validateAccept, type AcceptForm } from "../lib/muteForm";
import { acceptance } from "../test/fixtures/capacity";
import { renderApp } from "../test/render";
import type { Entity, Finding } from "../api/model";
import { AcceptDialog, type AcceptTarget } from "./AcceptDialog";

vi.mock("../api/requests", () => ({ muteFinding: vi.fn(), acceptFindings: vi.fn(), listFindings: vi.fn(), getLensInfo: vi.fn() }));
const muteFinding = vi.mocked(requests.muteFinding);
const acceptFindings = vi.mocked(requests.acceptFindings);
const listFindings = vi.mocked(requests.listFindings);
const getLensInfo = vi.mocked(requests.getLensInfo);
const info = (identityMode: "iap" | "request") => ({ identityMode, version: "dev", cluster: "c", collectIntervalMs: 30_000, retentionMs: 0, live: true, demo: true });

const target: AcceptTarget = {
  findingId: "stream-005:APP/order-archive",
  checkCode: "stream-005",
  checkName: "Stream without any limit",
  entity: { kind: "stream", key: "APP/order-archive", name: "order-archive", account: "APP", stream: "order-archive", consumer: "", server: "" },
  growthReview: true,
};

const NOW = Date.parse("2026-10-21T14:30:00Z");
const DAY = 86_400_000;
const form: AcceptForm = { reason: "Order history is kept for audits", review: "3mo", reviewDate: "", name: "alice" };

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
    acceptFindings.mockReset();
    listFindings.mockReset();
    listFindings.mockResolvedValue({ items: [], nextPageToken: "", totalCount: 0 });
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
    await userEvent.type(screen.getByLabelText("Your name"), "alice");
    fireEvent.change(screen.getByLabelText("Review date"), { target: { value: "1y" } });
    const before = Date.now();
    await userEvent.click(screen.getByRole("button", { name: "Accept as by design" }));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    const req = muteFinding.mock.calls[0][1];
    expect(req).toMatchObject({ checkCode: "stream-005", reason: "Order history is kept for audits", mutedBy: "alice", accept: true });
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
    await userEvent.type(screen.getByLabelText("Your name"), "alice");
    await userEvent.click(screen.getByRole("button", { name: "Accept as by design" }));
    expect(await screen.findByText(/365 days ahead/)).toBeInTheDocument();
    expect(onClose).not.toHaveBeenCalled();
  });
});

describe("AcceptDialog with related entities", () => {
  const consumer = (name: string): Entity => ({ kind: "consumer", key: `APP/order-jobs/${name}`, name, account: "APP", stream: "order-jobs", consumer: name, server: "" });
  const sibling = (name: string): Finding => ({
    id: `consumer-016:APP/order-jobs/${name}`,
    checkCode: "consumer-016",
    checkName: "Consumer with one replica",
    severity: "info",
    state: "firing",
    entity: consumer(name),
    detail: "",
    remediation: "",
    relatedConfig: [],
    relatedConfigSource: "",
    transitionsLastHour: 0,
    acceptable: true,
  });
  const one: AcceptTarget = { findingId: "consumer-016:APP/order-jobs/a", checkCode: "consumer-016", checkName: "Consumer with one replica", entity: consumer("a"), growthReview: false };

  beforeEach(() => {
    localStorage.clear();
    getLensInfo.mockResolvedValue(info("iap"));
    muteFinding.mockReset();
    acceptFindings.mockReset();
    listFindings.mockReset();
    listFindings.mockResolvedValue({ items: [sibling("a"), sibling("b"), sibling("c")], nextPageToken: "", totalCount: 3 });
  });

  it("offers the other consumers of the stream, unticked, and accepts the ticked ones with the target in one call", async () => {
    acceptFindings.mockResolvedValue([acceptance({ id: "1" }), acceptance({ id: "2" })]);
    const onClose = vi.fn();
    const onAccepted = vi.fn();
    renderApp(<AcceptDialog target={one} onClose={onClose} onAccepted={onAccepted} />);
    const all = await screen.findByRole("checkbox", { name: /All 2 other consumers of stream order-jobs with this finding/ });
    expect(all).not.toBeChecked();
    expect(listFindings.mock.calls[0][2]).toMatchObject({ checkCode: "consumer-016", includeMuted: false, entity: { kind: "stream", key: "APP/order-jobs" } });
    await userEvent.click(screen.getByRole("checkbox", { name: "c" }));
    expect(all).toBePartiallyChecked();
    await userEvent.type(screen.getByLabelText("Reason"), "batch workers run once");
    await userEvent.click(screen.getByRole("button", { name: "Accept 2 as by design" }));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(muteFinding).not.toHaveBeenCalled();
    const req = acceptFindings.mock.calls[0][1];
    expect(req.entities.map((e) => e.key)).toEqual(["APP/order-jobs/a", "APP/order-jobs/c"]);
    expect(req).toMatchObject({ checkCode: "consumer-016", reason: "batch workers run once" });
    expect(onAccepted.mock.calls.map((c) => c[0])).toEqual(["consumer-016:APP/order-jobs/a", "consumer-016:APP/order-jobs/c"]);
  });

  it("ticks the findings a card passes and leaves out, named, one the live read does not return", async () => {
    // The read returns a, b and c; the card passes b and d (d not in the read, as on a past range).
    renderApp(<AcceptDialog target={{ ...one, preselect: [sibling("b"), sibling("d")] }} onClose={() => {}} />);
    expect(await screen.findByText("Left out, no longer firing or muted since: d.")).toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: /All 2 other consumers/ })).toBePartiallyChecked();
    expect(screen.getByRole("checkbox", { name: "b" })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: "c" })).not.toBeChecked();
    expect(screen.queryByRole("checkbox", { name: "d" })).toBeNull();
    expect(screen.getByRole("button", { name: "Accept 2 as by design" })).toBeEnabled();
  });

  it("keeps a passed finding offered when the read has more pages and may hold it", async () => {
    listFindings.mockResolvedValue({ items: [sibling("a"), sibling("b")], nextPageToken: "next", totalCount: 600 });
    renderApp(<AcceptDialog target={{ ...one, preselect: [sibling("d")] }} onClose={() => {}} />);
    expect(await screen.findByRole("checkbox", { name: "b" })).not.toBeChecked();
    expect(screen.getByRole("checkbox", { name: /All 2 other consumers/ })).toBePartiallyChecked();
    expect(screen.getByRole("checkbox", { name: "d" })).toBeChecked();
    expect(screen.queryByText(/Left out/)).toBeNull();
  });

  it("shows a refusal once, with the account's name for its ID", async () => {
    const id = "AFAKEAPPACCOUNT";
    const typo: AcceptTarget = { ...target, findingId: `stream-005:${id}/typo`, entity: { ...target.entity, key: `${id}/typo`, name: "typo", stream: "typo", account: id, accountName: "APP" } };
    muteFinding.mockRejectedValue(new ConnectError(`not found: stream-005 for stream ${id}/typo: no finding of the check for the entity`, Code.NotFound));
    renderApp(<AcceptDialog target={typo} onClose={() => {}} />);
    await userEvent.type(screen.getByLabelText("Reason"), "kept");
    await userEvent.click(screen.getByRole("button", { name: "Accept as by design" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(/^not_found: stream-005 for stream APP\/typo: no finding of the check for the entity$/);
  });

  it("waits for the read when a card passed findings, and says when it fails", async () => {
    let resolve: (v: Awaited<ReturnType<typeof listFindings>>) => void = () => {};
    listFindings.mockReturnValue(new Promise((r) => (resolve = r)));
    renderApp(<AcceptDialog target={{ ...one, preselect: [sibling("b")] }} onClose={() => {}} />);
    expect(await screen.findByText("Loading related findings…")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Accept 2 as by design" })).toBeDisabled();
    resolve({ items: [sibling("a"), sibling("b"), sibling("c")], nextPageToken: "", totalCount: 3 });
    await waitFor(() => expect(screen.getByRole("button", { name: "Accept 2 as by design" })).toBeEnabled());
    expect(screen.getByRole("group", { name: "Also accept, firing now" })).toBeInTheDocument();
  });

  it("shows a failed read inside Also accept and keeps the card's findings ticked", async () => {
    listFindings.mockRejectedValue(new Error("unavailable"));
    renderApp(<AcceptDialog target={{ ...one, preselect: [sibling("b")] }} onClose={() => {}} />);
    expect(await screen.findByText(/The other findings of this check could not be read/)).toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: "b" })).toBeChecked();
    expect(screen.getByRole("button", { name: "Accept 2 as by design" })).toBeEnabled();
  });

  it("starts with every related entity ticked from a card's Accept all", async () => {
    renderApp(<AcceptDialog target={{ ...one, preselect: [sibling("b"), sibling("c")] }} onClose={() => {}} />);
    expect(await screen.findByRole("checkbox", { name: /All 2 other consumers/ })).toBeChecked();
    expect(screen.getByRole("button", { name: "Accept 3 as by design" })).toBeInTheDocument();
    expect(screen.getByText(/The 3 findings leave the Overview/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("checkbox", { name: /All 2 other consumers/ }));
    expect(screen.getByRole("button", { name: "Accept as by design" })).toBeInTheDocument();
  });

  it("offers nothing more when no related entity has the finding", async () => {
    listFindings.mockResolvedValue({ items: [sibling("a")], nextPageToken: "", totalCount: 1 });
    renderApp(<AcceptDialog target={one} onClose={() => {}} />);
    await waitFor(() => expect(listFindings).toHaveBeenCalled());
    await waitFor(() => expect(screen.queryByText("Loading related findings…")).toBeNull());
    expect(screen.queryByText("Also accept, firing now")).toBeNull();
  });
});
