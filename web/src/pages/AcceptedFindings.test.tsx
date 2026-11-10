import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Finding, Mute } from "../api/model";
import * as requests from "../api/requests";
import { acceptance } from "../test/fixtures/capacity";
import { renderApp } from "../test/render";
import { AcceptedFindings } from "./AcceptedFindings";
import { Findings } from "./Findings";

vi.mock("../api/requests", () => ({
  listAcceptances: vi.fn(),
  listFindings: vi.fn(),
  listMutes: vi.fn(),
  getLensInfo: vi.fn(() => new Promise(() => {})),
  getShellStatus: vi.fn(() => new Promise(() => {})),
}));
vi.mock("../api/checks", () => ({ useCheckDefinitions: () => ({ data: { definitions: [{ code: "stream-005", name: "Stream without any limit" }] } }) }));
const m = vi.mocked(requests);
const DAY = 86_400_000;

const page = (items: Mute[], otherAccountCount = 0) => ({ items, nextPageToken: "", totalCount: items.length, otherAccountCount });

beforeEach(() => {
  m.listAcceptances.mockReset();
  m.listFindings.mockReset();
  m.listMutes.mockReset();
});

describe("AcceptedFindings", () => {
  it("lists each acceptance with who, why, the review date and the growth at acceptance, and ends one", async () => {
    const a = acceptance({ expiresAt: Date.now() + 30 * DAY, growthPerDay: 96 * 1024 * 1024 });
    m.listAcceptances.mockResolvedValue(page([a]));
    renderApp(<AcceptedFindings />, "/findings/accepted");
    const why = await screen.findByText("Accepted by ops@example.com");
    expect(why.parentElement).toHaveTextContent(
      /^Accepted by ops@example\.com on \d{4}-\d{2}-\d{2} \d{2}:\d{2}: Customer records are kept for the account's life\. Review on \d{4}-\d{2}-\d{2}\. Growth at acceptance: \+96(\.0)? MiB\/day per copy\.$/,
    );
    expect(screen.getByText("Stream without any limit")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "stream-005" })).toHaveAttribute("href", expect.stringContaining("/findings/stream-005%3AAPP%2FKV_customer-config"));
    expect(m.listAcceptances.mock.calls[0][1]).toMatchObject({ includeEnded: false, account: "" });
    await userEvent.click(screen.getByRole("button", { name: "End acceptance of stream-005 for customer-config…" }));
    expect(screen.getByRole("dialog", { name: "End acceptance" })).toBeInTheDocument();
  });

  it("asks for the ended ones with Include ended and says when nats-lens ended one", async () => {
    m.listAcceptances.mockResolvedValue(page([]));
    renderApp(<AcceptedFindings />, "/findings/accepted");
    expect(await screen.findByText("No acceptance in force.")).toBeInTheDocument();
    const ended = acceptance({ unmutedAt: Date.now() - DAY, unmutedBy: "nats-lens", endReason: "growth changed from +2.0 KiB/day to +516 KiB/day over the last 7 days" });
    m.listAcceptances.mockResolvedValue(page([ended]));
    await userEvent.click(screen.getByRole("checkbox", { name: "Include ended" }));
    expect(await screen.findByText(/Ended by nats-lens: growth changed from/)).toBeInTheDocument();
    expect(screen.getByText("Ended", { selector: ".state, .sev, span" })).toBeInTheDocument();
    expect(m.listAcceptances.mock.calls.at(-1)![1]).toMatchObject({ includeEnded: true });
    expect(screen.getByTestId("location")).toHaveTextContent("ended=1");
  });

  it("follows the account scope and counts the other accounts' acceptances", async () => {
    m.listAcceptances.mockResolvedValue(page([acceptance({ expiresAt: Date.now() + DAY * 2 })], 2));
    renderApp(<AcceptedFindings />, "/findings/accepted?account=APP");
    expect(await screen.findByText(/2 acceptances of other accounts are not shown\./)).toBeInTheDocument();
    expect(m.listAcceptances.mock.calls[0][1]).toMatchObject({ account: "APP" });
  });
});

describe("Findings and acceptance", () => {
  const firing: Finding = {
    id: "stream-020:APP/audit",
    checkCode: "stream-020",
    checkName: "Stream without a consumer",
    severity: "info",
    state: "firing",
    entity: { kind: "stream", key: "APP/audit", name: "audit", account: "APP", stream: "audit", consumer: "", server: "" },
    since: Date.now() - 3_600_000,
    detail: "",
    remediation: "",
    relatedConfig: [],
    relatedConfigSource: "",
    transitionsLastHour: 0,
    acceptable: true,
  };

  it("offers Accept… on an acceptable row and links the review list", async () => {
    m.listFindings.mockResolvedValue({ items: [firing, { ...firing, id: "stream-002:APP/audit", checkCode: "stream-002", acceptable: false }], nextPageToken: "", totalCount: 2 });
    m.listMutes.mockResolvedValue({ items: [], nextPageToken: "", totalCount: 0 });
    renderApp(<Findings />, "/findings");
    const btn = await screen.findByRole("button", { name: "Accept stream-020 on audit as by design" });
    expect(screen.getAllByRole("button", { name: /^Accept stream-/ })).toHaveLength(1);
    expect(screen.getByRole("link", { name: "Review accepted findings" })).toHaveAttribute("href", expect.stringContaining("/findings/accepted"));
    await userEvent.click(btn);
    const dlg = screen.getByRole("dialog", { name: "Accept as by design" });
    expect(within(dlg).getByText(/^Accepts/)).toHaveTextContent("Accepts stream-020 Stream without a consumer for audit");
  });
});
