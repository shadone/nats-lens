// The command bar's Ask mode: a question copied with the page's agent
// prompt, through the same copy, dialog and retry as Copy as prompt.
import { create } from "@bufbuild/protobuf";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
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
    create(GetShellStatusResponseSchema, { systems: [{ system: { id: "sys-a", name: "A" } }], defaultSystem: "sys-a", systemKnown: true })) as never);
  m.listServers.mockResolvedValue({ servers: [{ name: "nats-1", cluster: "c" }], expected: [], missing: [] } as never);
  m.listStreams.mockResolvedValue({ streams: [{ entity: { account: "APP", stream: "order-jobs" } }] } as never);
  m.listConsumers.mockResolvedValue(Object.assign([], { totalCount: 0, nextPageToken: "" }));
  m.listFindings.mockResolvedValue(empty as never);
  ev.searchEvents.mockResolvedValue(empty as never);
});

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
