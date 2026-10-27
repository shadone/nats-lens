import { screen } from "@testing-library/react";
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

const a = "ADSUPPLIERSFAKEACCOUNTKEYEXAMPLEGLOBAL7Q2M4XKV3WPJ6RT5NH";
const b = "AXSUPPLIERSOTHERACCOUNTKEYEXAMPLEGLOBAL7Q2M4XKV3WPJ6RT5N";
const ent = (key: string) => ({ kind: "account" as const, key, name: key, account: key, accountName: "SUPPLIERS", stream: "", consumer: "", server: "" });

beforeEach(() => {
  m.listFindings.mockReset();
  m.listMutes.mockReset();
});

// Two SUPPLIERS accounts both fire and one is muted: the Findings list and
// the mutes list below it share one labeller, so the muted one reads the
// same in both. (A reviewer's case.)
describe("Findings: one labeller for the page", () => {
  it("names a muted account the same in the findings and the mutes list", async () => {
    const mute: Mute = { ...acceptance({ entity: ent(a) }), accepted: false, checkCode: "account-003" };
    const row = (key: string, mu?: Mute): Finding => ({
      id: `account-003:${key}`, checkCode: "account-003", checkName: "Account JWT expires soon", severity: "warning", state: "firing",
      entity: ent(key), since: Date.now() - 3_600_000, detail: "expires", remediation: "", relatedConfig: [], relatedConfigSource: "",
      transitionsLastHour: 0, mute: mu, expiry: true,
    });
    m.listFindings.mockResolvedValue(page([row(b), row(a, mute)]));
    m.listMutes.mockResolvedValue(page<Mute>([mute]));
    renderApp(<Findings />, "/findings");
    expect(await screen.findByText("SUPPLIERS (AXSU…)")).toBeInTheDocument();
    // The muted row in the findings list and its row in the mutes list:
    expect(screen.getAllByText("SUPPLIERS (ADSU…)").length).toBe(2);
    // The mute row names it as the findings list does:
    const lift = await screen.findByRole("button", { name: /Lift mute of account-003 for/ });
    expect(lift).toHaveAccessibleName("Lift mute of account-003 for SUPPLIERS (ADSU…)…");
  });
});
