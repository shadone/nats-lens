// Focus after an acceptance on the Capacity page.
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import * as requests from "../api/requests";
import { acceptance, prod } from "../test/fixtures/capacity";
import { renderApp } from "../test/render";
import { Capacity } from "./Capacity";

vi.mock("../api/requests", () => ({
  getCapacity: vi.fn(),
  muteFinding: vi.fn(),
  getLensInfo: vi.fn(() => Promise.resolve({ identityMode: "request", version: "dev", cluster: "c", collectIntervalMs: 30_000, retentionMs: 0, live: true, demo: true })),
  getShellStatus: vi.fn(() => Promise.reject(new Error("no shell in this test"))),
}));
const getCapacity = vi.mocked(requests.getCapacity);
const muteFinding = vi.mocked(requests.muteFinding);

beforeEach(() => localStorage.clear());
afterEach(() => {
  getCapacity.mockReset();
  muteFinding.mockReset();
});

describe("Capacity acceptance focus", () => {
  it("leaves keyboard focus inside the table after saving, not on the page body", async () => {
    getCapacity.mockResolvedValue(prod());
    muteFinding.mockResolvedValue(acceptance({ reason: "Kept for audits", mutedBy: "denis", expiresAt: Date.parse("2027-01-21T12:00:00Z") }));
    renderApp(<Capacity />, "/capacity");
    await userEvent.click(await screen.findByRole("button", { name: "Accept order-archive as by design…" }));
    const dlg = within(screen.getByRole("dialog"));
    await userEvent.type(dlg.getByLabelText("Reason"), "Kept for audits");
    await userEvent.type(dlg.getByLabelText("Your name"), "denis");
    await userEvent.click(dlg.getByRole("button", { name: "Accept as by design" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(document.activeElement).not.toBe(document.body);
  });
});
