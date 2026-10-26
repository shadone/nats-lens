import { screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Finding, Mute } from "../api/model";
import * as requests from "../api/requests";
import { acceptance } from "../test/fixtures/capacity";
import { renderApp } from "../test/render";
import { Findings } from "./Findings";

vi.mock("../api/requests", () => ({
  listFindings: vi.fn(),
  listMutes: vi.fn(),
  getLensInfo: vi.fn(() => new Promise(() => {})),
  getShellStatus: vi.fn(() => new Promise(() => {})),
}));
const m = vi.mocked(requests);

const page = <T,>(items: T[]) => ({ items, nextPageToken: "", totalCount: items.length });

const finding = (mute: Mute): Finding => ({
  id: "stream-005:APP/KV_customer-config",
  checkCode: "stream-005",
  checkName: "Stream without any limit",
  severity: "info",
  state: "firing",
  entity: mute.entity,
  since: Date.now() - 3_600_000,
  detail: "keys accumulate without bound",
  remediation: "",
  relatedConfig: [],
  relatedConfigSource: "",
  transitionsLastHour: 0,
  mute,
});

beforeEach(() => {
  vi.mocked(m.listFindings).mockReset();
  vi.mocked(m.listMutes).mockReset();
});

describe("Findings with acceptances", () => {
  it("reads an accepted finding as accepted, with its review date", async () => {
    const a = acceptance({ expiresAt: Date.now() + 100 * 86_400_000 });
    m.listFindings.mockResolvedValue(page([finding(a)]));
    m.listMutes.mockResolvedValue(page<Mute>([]));
    renderApp(<Findings />, "/findings");
    const why = await screen.findByText("Accepted by ops@example.com:");
    expect(why.parentElement).toHaveTextContent(/^Accepted by ops@example\.com: Customer records are kept for the account's life\. Review on \d{4}-\d{2}-\d{2}\.$/);
    expect(screen.getAllByText("Accepted", { selector: ".sev" })).toHaveLength(1);
  });

  it("lists an acceptance in force as Accepted, and one nats-lens ended with its reason", async () => {
    m.listFindings.mockResolvedValue(page<Finding>([]));
    const inForce = acceptance({ expiresAt: Date.now() + 100 * 86_400_000 });
    const ended = acceptance({
      id: "m2",
      unmutedAt: Date.now() - 86_400_000,
      unmutedBy: "nats-lens",
      endReason: "growth changed from +2.0 KiB/day to +520 KiB/day over the last 7 days (+1.6 MiB in the last day)",
    });
    m.listMutes.mockResolvedValue(page([inForce, ended]));
    renderApp(<Findings />, "/findings");
    const list = within((await screen.findAllByText(/^Accepted by ops@example.com/))[0].closest(".mutes-list") as HTMLElement);
    const rows = list.getAllByText(/^Accepted by/).map((b) => b.closest(".frow") as HTMLElement);
    expect(within(rows[0]).getByText("Accepted", { selector: ".sev" })).toBeInTheDocument();
    expect(within(rows[0]).getByRole("button", { name: "End acceptance of stream-005 for KV_customer-config…" })).toHaveTextContent(/^End…$/);
    expect(within(rows[1]).getByText("Ended", { selector: ".sev" })).toBeInTheDocument();
    expect(rows[1]).toHaveTextContent("Ended by nats-lens: growth changed from +2.0 KiB/day to +520 KiB/day over the last 7 days (+1.6 MiB in the last day).");
    expect(rows[1]).not.toHaveTextContent("Lifted");
  });

  it("words an acceptance a person ended as ended, and names a mute's lift button with its entity", async () => {
    m.listFindings.mockResolvedValue(page<Finding>([]));
    const ended = acceptance({ unmutedAt: Date.parse("2026-10-21T10:00:00Z"), unmutedBy: "reviewer" });
    const mute = acceptance({ id: "m3", accepted: false, checkCode: "stream-006", expiresAt: Date.now() + 86_400_000 });
    m.listMutes.mockResolvedValue(page([ended, mute]));
    renderApp(<Findings />, "/findings");
    const row = (await screen.findByText(/^Accepted by/)).closest(".frow") as HTMLElement;
    expect(within(row).getByText("Ended", { selector: ".sev" })).toBeInTheDocument();
    expect(row).toHaveTextContent("Ended 2026-10-21 10:00 by reviewer.");
    expect(row).not.toHaveTextContent("Lifted");
    expect(screen.getByRole("button", { name: "Lift mute of stream-006 for KV_customer-config…" })).toHaveTextContent(/^Lift…$/);
  });

  it("names an account finding and its mute by the account's name, not its key", async () => {
    const key = "ADSUPPLIERSFAKEACCOUNTKEYEXAMPLEGLOBAL7Q2M4XKV3WPJ6RT5NH";
    const entity = { kind: "account" as const, key, name: key, account: key, accountName: "SUPPLIERS", stream: "", consumer: "", server: "" };
    const mute = acceptance({ id: "m4", accepted: false, checkCode: "account-003", entity, expiresAt: Date.now() + 86_400_000 });
    m.listFindings.mockResolvedValue(page([{ ...finding(mute), id: `account-003:${key}`, checkCode: "account-003", checkName: "Account JWT expires soon" }]));
    m.listMutes.mockResolvedValue(page([mute]));
    renderApp(<Findings />, "/findings");
    expect(await screen.findByRole("button", { name: "Lift mute of account-003 for SUPPLIERS…" })).toBeInTheDocument();
    expect(screen.getAllByText("SUPPLIERS").length).toBeGreaterThan(0);
    expect(screen.queryByText(key)).toBeNull();
  });
});
