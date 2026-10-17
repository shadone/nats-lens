import { fireEvent, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { lens } from "../api/client";
import * as requests from "../api/requests";
import { renderApp } from "../test/render";
import { isSearchShortcut, SearchJump } from "./SearchJump";

vi.mock("../api/client", () => ({
  lens: {
    listSystems: vi.fn(),
    listAccounts: vi.fn(async () => ({ accounts: [] })),
    listKeyValueStores: vi.fn(async () => ({ keyValueStores: [] })),
    listObjectStores: vi.fn(async () => ({ objectStores: [] })),
  },
}));
vi.mock("../api/requests", async (orig) => ({
  ...(await orig<typeof import("../api/requests")>()),
  listServers: vi.fn(),
  listStreams: vi.fn(),
  listConsumers: vi.fn(),
}));
const m = vi.mocked(requests);

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(lens.listSystems).mockResolvedValue({ systems: [{ system: { id: "sys-a", name: "A", environment: "" } }], defaultSystem: "sys-a" } as never);
  m.listServers.mockResolvedValue({ servers: [{ name: "nats-1", cluster: "c" }], expected: [], missing: [] } as never);
  m.listStreams.mockResolvedValue({ streams: [{ entity: { account: "APP", stream: "order-jobs" } }, { entity: { account: "APP", stream: "order-archive" } }] } as never);
  m.listConsumers.mockResolvedValue([]);
});

const box = () => screen.getByRole("combobox", { name: "Search this system" });

describe("isSearchShortcut", () => {
  const ev = (o: Partial<KeyboardEvent>) => ({ key: "k", metaKey: false, ctrlKey: false, ...o }) as KeyboardEvent;
  it("is Cmd+K on a Mac, where Ctrl+K kills the line", () => {
    expect(isSearchShortcut(ev({ metaKey: true }), true)).toBe(true);
    expect(isSearchShortcut(ev({ ctrlKey: true }), true)).toBe(false);
  });
  it("is Ctrl+K elsewhere", () => {
    expect(isSearchShortcut(ev({ ctrlKey: true }), false)).toBe(true);
    expect(isSearchShortcut(ev({ metaKey: true }), false)).toBe(false);
    expect(isSearchShortcut(ev({ ctrlKey: true, key: "j" }), false)).toBe(false);
  });
});

describe("SearchJump", () => {
  it("takes focus on the shortcut, but not from a text field", () => {
    const mac = /Mac|iPhone|iPad/.test(navigator.platform);
    const shortcut = mac ? { metaKey: true } : { ctrlKey: true };
    renderApp(
      <>
        <SearchJump />
        <input aria-label="Other" />
      </>,
      "/?system=sys-a",
    );
    fireEvent.keyDown(document.body, { key: "k", ...shortcut });
    expect(box()).toHaveFocus();
    const other = screen.getByLabelText("Other");
    other.focus();
    fireEvent.keyDown(other, { key: "k", ...shortcut });
    expect(other).toHaveFocus();
  });

  it("is a combobox whose list exists only while open, with the active option named", async () => {
    renderApp(<SearchJump />, "/?system=sys-a");
    expect(box()).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByRole("listbox")).toBeNull();
    await userEvent.type(box(), "order");
    const options = await screen.findAllByRole("option");
    expect(options.map((o) => o.textContent)).toEqual(["Streamorder-archiveAPP", "Streamorder-jobsAPP"]);
    expect(box()).toHaveAttribute("aria-expanded", "true");
    expect(box()).toHaveAttribute("aria-controls", screen.getByRole("listbox").id);
    expect(box()).toHaveAttribute("aria-activedescendant", options[0].id);
    await userEvent.keyboard("{ArrowDown}");
    expect(box()).toHaveAttribute("aria-activedescendant", options[1].id);
    expect(options[1]).toHaveAttribute("aria-selected", "true");
  });

  it("closes on the first Escape, clears on the second, and keeps focus", async () => {
    renderApp(<SearchJump />, "/?system=sys-a");
    await userEvent.type(box(), "order");
    await screen.findAllByRole("option");
    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("listbox")).toBeNull();
    expect(box()).toHaveValue("order");
    await userEvent.keyboard("{Escape}");
    expect(box()).toHaveValue("");
    expect(box()).toHaveFocus();
    await userEvent.keyboard("{Escape}");
    expect(box()).toHaveFocus();
  });

  it("asks nothing of a system that is not configured", async () => {
    renderApp(<SearchJump />, "/?system=nope");
    await waitFor(() => expect(lens.listSystems).toHaveBeenCalled());
    await userEvent.type(box(), "order");
    await new Promise((r) => setTimeout(r, 50));
    expect(m.listServers).not.toHaveBeenCalled();
  });
});
