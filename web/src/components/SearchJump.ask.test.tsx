// The command bar's Ask mode: a question copied with the page's agent
// prompt, through the same copy, dialog and retry as Copy as prompt.
import { create } from "@bufbuild/protobuf";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { readFileSync } from "node:fs";
import { Link } from "react-router";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { lens } from "../api/client";
import * as events from "../api/events";
import * as requests from "../api/requests";
import { GetShellStatusResponseSchema } from "../gen/natslens/v1/systems_pb";
import { renderApp } from "../test/render";
import { InvestigationProvider } from "./InvestigationContext";
import { SearchJump } from "./SearchJump";

vi.mock("../api/client", () => ({
  lens: {
    getShellStatus: vi.fn(),
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
  listFindings: vi.fn(),
}));
vi.mock("../api/events", async (orig) => ({
  ...(await orig<typeof import("../api/events")>()),
  searchEvents: vi.fn(),
}));
vi.mock("../api/queries", async (orig) => ({
  ...(await orig<typeof import("../api/queries")>()),
  useLensInfo: () => ({ data: { version: "fixture" } }),
}));
const m = vi.mocked(requests);
const ev = vi.mocked(events);
const write = vi.fn();
const empty = { items: [], nextPageToken: "", totalCount: 0 };

beforeEach(() => {
  vi.clearAllMocks();
  write.mockReset().mockResolvedValue(undefined);
  Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText: write } });
  vi.mocked(lens.getShellStatus).mockImplementation((async () =>
    create(GetShellStatusResponseSchema, { systems: [{ system: { id: "sys-a", name: "A" } }, { system: { id: "sys-b", name: "B" } }], defaultSystem: "sys-a", systemKnown: true })) as never);
  m.listServers.mockResolvedValue({ servers: [{ name: "nats-1", cluster: "c" }], expected: [], missing: [] } as never);
  m.listStreams.mockResolvedValue({ streams: [{ entity: { account: "APP", stream: "order-jobs" } }] } as never);
  m.listConsumers.mockResolvedValue(Object.assign([], { totalCount: 0, nextPageToken: "" }));
  m.listFindings.mockResolvedValue(empty as never);
  ev.searchEvents.mockResolvedValue(empty as never);
});

// Through a function: vite rewrites a literal new URL(..., import.meta.url) into a served URL.
const read = (f: string) => readFileSync(new URL(f, import.meta.url), "utf8");
const box = () => screen.getByRole("combobox", { name: "Search this system" });
const askRow = () => screen.getByRole("option", { name: /Copy prompt with question/ });
const lines = (p: string) => p.split("\n");
const questionOf = (p: string) => JSON.parse(lines(p)[lines(p).indexOf("BEGIN USER QUESTION") + 1]);
const contextOf = (p: string) => JSON.parse(p.split("BEGIN NATS LENS CONTEXT JSON\n")[1].split("\nEND NATS LENS CONTEXT JSON")[0]);
const setup = (route: string) => renderApp(<InvestigationProvider><SearchJump /></InvestigationProvider>, route);
/** Typed in one change, as a paste would be: userEvent would take seconds for a long text. */
const enter = async (text: string) => {
  await userEvent.click(box());
  fireEvent.change(box(), { target: { value: text } });
};

describe("Ask your agent", () => {
  it("copies the page's prompt with the question after ask:, and searches nothing", async () => {
    setup("/servers/nats-1?system=sys-a");
    await enter("ask: why is nats-1 slow since:2h stream: order?");
    await waitFor(() => expect(askRow()).toHaveAttribute("aria-selected", "true"));
    expect(screen.getAllByRole("option")).toHaveLength(1);
    expect(screen.getByText("Ask your agent", { selector: ".search-chip" })).toBeInTheDocument();
    expect(screen.getByText(/up to 2,000 characters, for your own agent/)).toBeInTheDocument();
    await userEvent.keyboard("{Enter}");
    await waitFor(() => expect(write).toHaveBeenCalledOnce());
    const prompt = write.mock.calls[0][0] as string;
    expect(questionOf(prompt)).toBe("why is nats-1 slow since:2h stream: order?");
    expect(prompt.indexOf("Table-local filters are unavailable.")).toBeLessThan(prompt.indexOf("BEGIN USER QUESTION"));
    expect(prompt.indexOf("END USER QUESTION")).toBeLessThan(prompt.indexOf("BEGIN NATS LENS CONTEXT JSON"));
    expect(prompt).toContain("asked on the NATS Lens page in sourcePath");
    expect(contextOf(prompt)).toMatchObject({ system: "sys-a", sourcePath: "/servers/nats-1", entity: { type: "ENTITY_TYPE_SERVER", key: "nats-1" } });
    await screen.findByText("Prompt copied");
    expect(box()).toHaveValue("");
    expect(box()).toHaveFocus();
    expect(screen.queryByRole("listbox")).toBeNull();
    await new Promise((r) => setTimeout(r, 300));
    expect(m.listFindings).not.toHaveBeenCalled();
    expect(ev.searchEvents).not.toHaveBeenCalled();
  });

  it("offers Ask as the last row after the results, and says when the page adds no context", async () => {
    setup("/?system=sys-a");
    await enter("order jobs lag");
    await waitFor(() => expect(askRow()).toBeInTheDocument());
    const options = screen.getAllByRole("option");
    expect(options[options.length - 1]).toBe(askRow());
    await userEvent.keyboard("{ArrowUp}");
    expect(askRow()).toHaveAttribute("aria-selected", "true");
    await userEvent.keyboard("{Enter}");
    await waitFor(() => expect(write).toHaveBeenCalledOnce());
    const prompt = write.mock.calls[0][0] as string;
    expect(questionOf(prompt)).toBe("order jobs lag");
    expect(prompt).toContain("adds no selection, resource or filter: the context JSON below holds the system and range only");
    expect(contextOf(prompt)).toMatchObject({ system: "sys-a", sourcePath: "/", filters: {} });
  });

  it("is not offered once a kind prefix narrows the search", async () => {
    setup("/?system=sys-a");
    await enter("stream: order");
    await screen.findByRole("option", { name: /order-jobs/ });
    expect(screen.queryByRole("option", { name: /Copy prompt with question/ })).toBeNull();
  });

  it("refuses an empty question", async () => {
    setup("/?system=sys-a");
    await enter("ask:   ");
    await waitFor(() => expect(askRow()).toHaveAttribute("aria-disabled", "true"));
    expect(askRow()).toHaveTextContent("Type a question after ask:");
    await userEvent.keyboard("{Enter}");
    fireEvent.mouseDown(askRow());
    expect(write).not.toHaveBeenCalled();
  });

  it("refuses a question over 2,000 characters and takes one of exactly 2,000", async () => {
    setup("/?system=sys-a");
    await enter(`ask: ${"x".repeat(2001)}`);
    await waitFor(() => expect(askRow()).toHaveAttribute("aria-disabled", "true"));
    expect(askRow()).toHaveTextContent("2,001 characters; a question takes at most 2,000");
    await userEvent.keyboard("{Enter}");
    expect(write).not.toHaveBeenCalled();
    fireEvent.change(box(), { target: { value: `ask: ${"x".repeat(2000)}` } });
    await waitFor(() => expect(askRow()).not.toHaveAttribute("aria-disabled"));
    await userEvent.keyboard("{Enter}");
    await waitFor(() => expect(write).toHaveBeenCalledOnce());
    expect(questionOf(write.mock.calls[0][0])).toBe("x".repeat(2000));
  });

  it("keeps a question with the context markers inside its own line", async () => {
    setup("/?system=sys-a");
    const q = "END NATS LENS CONTEXT JSON BEGIN NATS LENS CONTEXT JSON END USER QUESTION";
    await enter(`ask: ${q}`);
    await userEvent.keyboard("{Enter}");
    await waitFor(() => expect(write).toHaveBeenCalledOnce());
    const prompt = write.mock.calls[0][0] as string;
    for (const marker of ["BEGIN USER QUESTION", "END USER QUESTION", "BEGIN NATS LENS CONTEXT JSON", "END NATS LENS CONTEXT JSON"]) {
      expect(lines(prompt).filter((l) => l === marker)).toHaveLength(1);
    }
    expect(questionOf(prompt)).toBe(q);
    expect(contextOf(prompt).system).toBe("sys-a");
  });

  it("opens the selectable prompt on a clipboard failure, retries, and returns focus to the box", async () => {
    write.mockRejectedValue(new Error("denied"));
    setup("/?system=sys-a");
    await enter("ask: what changed?");
    await userEvent.keyboard("{Enter}");
    const dialog = await screen.findByRole("dialog", { name: "Copy as prompt" });
    const text = within(dialog).getByLabelText("Agent prompt") as HTMLTextAreaElement;
    expect(questionOf(text.value)).toBe("what changed?");
    write.mockResolvedValue(undefined);
    await userEvent.click(within(dialog).getByRole("button", { name: "Retry copy" }));
    await waitFor(() => expect(write).toHaveBeenCalledTimes(2));
    expect(write.mock.calls[1][0]).toBe(text.value);
    await screen.findByText("Prompt copied");
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(box()).toHaveFocus();
  });

  it("returns focus to the box when the fallback is closed with Escape", async () => {
    write.mockRejectedValue(new Error("denied"));
    setup("/?system=sys-a");
    await enter("ask: what changed?");
    await userEvent.keyboard("{Enter}");
    const dialog = await screen.findByRole("dialog", { name: "Copy as prompt" });
    fireEvent(dialog, new Event("cancel", { cancelable: true }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(box()).toHaveFocus();
  });

  it("takes the question back as search text when its chip is removed", async () => {
    setup("/?system=sys-a");
    await enter("ask: order");
    fireEvent.click(screen.getByRole("button", { name: "Remove ask your agent" }));
    expect(box()).toHaveValue("order");
  });

  it("copies with a click on the row", async () => {
    setup("/?system=sys-a");
    await enter("ask: why");
    fireEvent.mouseDown(askRow());
    await waitFor(() => expect(write).toHaveBeenCalledOnce());
    expect(questionOf(write.mock.calls[0][0])).toBe("why");
  });
});

describe("the active row", () => {
  it("scrolls into view when the arrow keys reach it, such as Ask at the end of a long list", async () => {
    const scroll = vi.fn();
    Object.defineProperty(HTMLElement.prototype, "scrollIntoView", { configurable: true, value: scroll });
    try {
      setup("/?system=sys-a");
      await enter("order");
      await waitFor(() => expect(askRow()).toBeInTheDocument());
      scroll.mockClear();
      await userEvent.keyboard("{ArrowUp}");
      expect(askRow()).toHaveAttribute("aria-selected", "true");
      expect(scroll.mock.contexts).toContain(askRow());
      expect(scroll).toHaveBeenLastCalledWith({ block: "nearest" });
    } finally {
      delete (HTMLElement.prototype as { scrollIntoView?: unknown }).scrollIntoView;
    }
  });
});

describe("Enter before the results settle", () => {
  it("does not copy when Enter comes before the names have loaded", async () => {
    m.listServers.mockReturnValue(new Promise(() => {}) as never);
    m.listStreams.mockReturnValue(new Promise(() => {}) as never);
    setup("/?system=sys-a");
    await enter("nats-1");
    await screen.findByRole("option", { name: /Copy prompt with question/ });
    await userEvent.keyboard("{Enter}");
    await new Promise((r) => setTimeout(r, 50));
    expect(write).not.toHaveBeenCalled();
    expect(box()).toHaveValue("nats-1");
  });

  it("does not copy when Enter comes before a finding search settles", async () => {
    m.listFindings.mockResolvedValue({ items: [{ id: "f1", checkCode: "stream-002", checkName: "lag", entity: { type: 2, key: "APP/order-jobs" }, severity: 2, detail: "zzlag" }], nextPageToken: "", totalCount: 1 } as never);
    setup("/?system=sys-a");
    await enter("zzlag");
    await userEvent.keyboard("{Enter}");
    await new Promise((r) => setTimeout(r, 50));
    expect(write).not.toHaveBeenCalled();
  });

  it("copies from the last row once the pointer has moved to it", async () => {
    m.listServers.mockReturnValue(new Promise(() => {}) as never);
    setup("/?system=sys-a");
    await enter("nats-1");
    fireEvent.mouseEnter(await screen.findByRole("option", { name: /Copy prompt with question/ }));
    await userEvent.keyboard("{Enter}");
    await waitFor(() => expect(write).toHaveBeenCalledOnce());
    expect(questionOf(write.mock.calls[0][0])).toBe("nats-1");
  });
});

describe("Ask and the page", () => {
  it("leaves tokens and unknown query parameters out of the prompt", async () => {
    setup("/servers/nats-1?system=sys-a&token=SEKRIT123&api_key=SEKRIT456#tok=SEKRIT789");
    await enter("ask: why");
    await userEvent.keyboard("{Enter}");
    await waitFor(() => expect(write).toHaveBeenCalledOnce());
    expect(write.mock.calls[0][0]).not.toMatch(/SEKRIT/);
  });

  it("takes the context of the page after a route change and a system change", async () => {
    renderApp(<InvestigationProvider><Link to="/servers/nats-1?system=sys-b">go</Link><SearchJump /></InvestigationProvider>, "/?system=sys-a");
    await enter("ask: first");
    await userEvent.keyboard("{Enter}");
    await waitFor(() => expect(write).toHaveBeenCalledTimes(1));
    expect(contextOf(write.mock.calls[0][0])).toMatchObject({ system: "sys-a", sourcePath: "/" });
    await userEvent.click(screen.getByText("go"));
    await enter("ask: second");
    await waitFor(() => expect(askRow()).toBeInTheDocument());
    await userEvent.keyboard("{Enter}");
    await waitFor(() => expect(write).toHaveBeenCalledTimes(2));
    expect(contextOf(write.mock.calls[1][0])).toMatchObject({ system: "sys-b", sourcePath: "/servers/nats-1" });
    expect(questionOf(write.mock.calls[1][0])).toBe("second");
  });
});

describe("Ask and the other prefixes", () => {
  it("is not offered after event:ask:", async () => {
    setup("/?system=sys-a");
    await enter("event:ask:why");
    await new Promise((r) => setTimeout(r, 400));
    expect(screen.queryByRole("option", { name: /Copy prompt with question/ })).toBeNull();
  });

  it("makes since:2h the question after ask:", async () => {
    setup("/?system=sys-a");
    await enter("ask:since:2h");
    await userEvent.keyboard("{Enter}");
    await waitFor(() => expect(write).toHaveBeenCalledOnce());
    expect(questionOf(write.mock.calls[0][0])).toBe("since:2h");
  });

  it("keeps since: as typed in a question from the last row", async () => {
    setup("/?system=sys-a");
    await enter("since:2h order lag");
    await userEvent.keyboard("{ArrowUp}{Enter}");
    await waitFor(() => expect(write).toHaveBeenCalledOnce());
    expect(questionOf(write.mock.calls[0][0])).toBe("since:2h order lag");
  });
});

describe("the dimmed Ask row", () => {
  it("is dimmed while its question cannot be asked", async () => {
    setup("/?system=sys-a");
    await enter("ask:");
    expect(await screen.findByRole("option", { name: /Copy prompt with question/ })).toHaveAttribute("aria-disabled", "true");
    const css = read("../styles/header.css");
    expect(css).toMatch(/\.search-pop li\.search-ask\[aria-disabled="true"\] \.sn\{color:var\(--text-muted\)\}/);
    fireEvent.change(box(), { target: { value: "ask: why" } });
    await waitFor(() => expect(askRow()).not.toHaveAttribute("aria-disabled"));
  });

  it("gives its reason to the box, for a screen reader", async () => {
    setup("/?system=sys-a");
    await enter("ask:");
    await screen.findByRole("option", { name: /Copy prompt with question/ });
    expect(box()).toHaveAccessibleDescription("Type a question after ask:");
    fireEvent.change(box(), { target: { value: `ask: ${"x".repeat(2001)}` } });
    await waitFor(() => expect(box()).toHaveAccessibleDescription("2,001 characters; a question takes at most 2,000"));
    fireEvent.change(box(), { target: { value: "ask: why" } });
    await waitFor(() => expect(box()).not.toHaveAttribute("aria-describedby"));
  });
});

describe("scrolling the list", () => {
  it("does not scroll when the pointer moves onto a row", async () => {
    const scroll = vi.fn();
    Object.defineProperty(HTMLElement.prototype, "scrollIntoView", { configurable: true, value: scroll });
    try {
      setup("/?system=sys-a");
      await enter("order");
      await waitFor(() => expect(askRow()).toBeInTheDocument());
      scroll.mockClear();
      fireEvent.mouseEnter(askRow());
      expect(askRow()).toHaveAttribute("aria-selected", "true");
      expect(scroll).not.toHaveBeenCalled();
    } finally {
      delete (HTMLElement.prototype as { scrollIntoView?: unknown }).scrollIntoView;
    }
  });
});

describe("the note under the Ask row", () => {
  it("says the question goes nowhere after ask:, and that text without it was searched", async () => {
    setup("/?system=sys-a");
    await enter("ask: why");
    expect(await screen.findByText(/nats-lens sends the question nowhere\./)).toBeInTheDocument();
    fireEvent.change(box(), { target: { value: "why" } });
    expect(await screen.findByText(/text without ask: is also a search, which asks nats-lens's own API for findings and events; start with ask: to skip it\./)).toBeInTheDocument();
    expect(screen.queryByText(/sends the question nowhere/)).toBeNull();
  });
});

describe("which row Enter picks", () => {
  const loc = () => screen.getByTestId("location").textContent;
  const settle = () => new Promise((r) => setTimeout(r, 400));

  it("shows no row active when the Ask row of text without ask: is the only one", async () => {
    setup("/?system=sys-a");
    await enter("zzz nothing");
    await screen.findByRole("option", { name: /Copy prompt with question/ });
    await settle();
    expect(screen.getAllByRole("option")).toHaveLength(1);
    expect(askRow()).toHaveAttribute("aria-selected", "false");
    expect(askRow()).not.toHaveClass("on");
    expect(box()).not.toHaveAttribute("aria-activedescendant");
    await userEvent.keyboard("{Enter}");
    await new Promise((r) => setTimeout(r, 50));
    expect(write).not.toHaveBeenCalled();
    expect(box()).toHaveValue("zzz nothing");
    await userEvent.keyboard("{ArrowDown}");
    expect(askRow()).toHaveAttribute("aria-selected", "true");
    expect(box()).toHaveAttribute("aria-activedescendant", askRow().id);
  });

  it("keeps the row moved to when results arrive after it", async () => {
    let names: (v: unknown) => void = () => {};
    let streams: (v: unknown) => void = () => {};
    m.listServers.mockReturnValue(new Promise((r) => (names = r)) as never);
    m.listStreams.mockReturnValue(new Promise((r) => (streams = r)) as never);
    setup("/?system=sys-a");
    await enter("nats-1");
    await screen.findByRole("option", { name: /Copy prompt with question/ });
    await userEvent.keyboard("{ArrowUp}");
    expect(askRow()).toHaveAttribute("aria-selected", "true");
    names({ servers: [{ name: "nats-1", cluster: "c" }], expected: [], missing: [] });
    streams({ streams: [] });
    await screen.findByRole("option", { name: /nats-1/, selected: false });
    expect(askRow()).toHaveAttribute("aria-selected", "true");
    await userEvent.keyboard("{Enter}");
    await waitFor(() => expect(write).toHaveBeenCalledOnce());
    expect(questionOf(write.mock.calls[0][0])).toBe("nats-1");
  });

  it("still opens the first result on Enter with no move", async () => {
    setup("/?system=sys-a");
    await enter("nats-1");
    await screen.findByRole("option", { name: /^Server/ });
    await settle();
    await userEvent.keyboard("{Enter}");
    await waitFor(() => expect(loc()).toMatch(/^\/servers\/nats-1/));
    expect(write).not.toHaveBeenCalled();
  });

  it("forgets the move when the text changes, after the arrow keys or the pointer", async () => {
    setup("/?system=sys-a");
    await enter("zzz");
    await screen.findByRole("option", { name: /Copy prompt with question/ });
    await userEvent.keyboard("{ArrowDown}");
    fireEvent.change(box(), { target: { value: "zzzq" } });
    await userEvent.keyboard("{Enter}");
    fireEvent.mouseEnter(askRow());
    fireEvent.change(box(), { target: { value: "zzzqq" } });
    await userEvent.keyboard("{Enter}");
    await new Promise((r) => setTimeout(r, 50));
    expect(write).not.toHaveBeenCalled();
    expect(box()).toHaveValue("zzzqq");
  });

  it("ignores keys while an input method composes", async () => {
    setup("/?system=sys-a");
    await enter("ask: why");
    await screen.findByRole("option", { name: /Copy prompt with question/ });
    fireEvent.keyDown(box(), { key: "Enter", isComposing: true });
    fireEvent.keyDown(box(), { key: "Enter", keyCode: 229 });
    fireEvent.keyDown(box(), { key: "Escape", isComposing: true });
    await new Promise((r) => setTimeout(r, 50));
    expect(write).not.toHaveBeenCalled();
    expect(screen.getByRole("listbox")).toBeInTheDocument();
    fireEvent.keyDown(box(), { key: "Enter" });
    await waitFor(() => expect(write).toHaveBeenCalledOnce());
  });

  it("ties a unique reason to the box only while the list shows", async () => {
    setup("/?system=sys-a");
    await enter(`ab ${"x".repeat(2001)}`);
    await waitFor(() => expect(box()).toHaveAttribute("aria-describedby"));
    const id = box().getAttribute("aria-describedby")!;
    expect(document.querySelectorAll(`[id="${id}"]`)).toHaveLength(1);
    expect(box()).toHaveAccessibleDescription(/2,004 characters/);
    await userEvent.keyboard("{Escape}");
    expect(box()).not.toHaveAttribute("aria-describedby");
    expect(box()).not.toHaveAttribute("aria-activedescendant");
  });
});

describe("moving through rows", () => {
  const settle = () => new Promise((r) => setTimeout(r, 400));
  const selected = () => screen.getAllByRole("option").filter((o) => o.getAttribute("aria-selected") === "true");

  it("passes two servers that share a name with ArrowDown", async () => {
    m.listServers.mockResolvedValue({ servers: [{ name: "nats-1", cluster: "east" }, { name: "nats-1", cluster: "west" }], expected: [], missing: [] } as never);
    m.listStreams.mockResolvedValue({ streams: [] } as never);
    setup("/?system=sys-a");
    await enter("nats-1");
    await screen.findAllByRole("option", { name: /^Server/ });
    await settle();
    expect(screen.getAllByRole("option")).toHaveLength(3);
    await userEvent.keyboard("{ArrowDown}");
    expect(selected()).toHaveLength(1);
    expect(selected()[0]).toBe(screen.getAllByRole("option")[1]);
    await userEvent.keyboard("{ArrowDown}");
    expect(selected()[0]).toBe(askRow());
  });

  it("names an existing, selected row in aria-activedescendant at every step", async () => {
    setup("/?system=sys-a");
    await enter("nats");
    await screen.findAllByRole("option", { name: /^Server/ });
    await settle();
    for (const k of ["{ArrowDown}", "{ArrowDown}", "{ArrowDown}", "{ArrowUp}", "{ArrowUp}", "{ArrowUp}", "{ArrowUp}"]) {
      await userEvent.keyboard(k);
      const id = box().getAttribute("aria-activedescendant");
      expect(id).toBeTruthy();
      expect(document.getElementById(id!)).not.toBeNull();
      expect(selected()).toHaveLength(1);
      expect(selected()[0].id).toBe(id);
    }
  });
});
