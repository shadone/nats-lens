// The command bar's findings, events, groups and prefixes; the name search
// and the shortcut are in SearchJump.test.tsx.
import { create } from "@bufbuild/protobuf";
import { act, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { lens } from "../api/client";
import type { Finding, LensEvent } from "../api/model";
import * as requests from "../api/requests";
import { GetShellStatusResponseSchema } from "../gen/natslens/v1/systems_pb";
import { serverNow } from "../lib/clock";
import { hm } from "../lib/format";
import { renderApp } from "../test/render";
import { SEARCH_DEBOUNCE_MS, SearchJump } from "./SearchJump";

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
  listEvents: vi.fn(),
}));
const m = vi.mocked(requests);

/** An hour ago, so that the rows show the time without a date. */
const AT = serverNow() - 3_600_000;
const FINDING = {
  id: "consumer-010:APP/order-archive/analytics-aggregator",
  checkCode: "consumer-010",
  checkName: "Expected durable missing",
  severity: "warning",
  state: "firing",
  entity: { kind: "consumer", key: "APP/order-archive/analytics-aggregator", name: "analytics-aggregator", account: "APP", stream: "order-archive", consumer: "analytics-aggregator", server: "" },
  since: AT,
} as Finding;
const EVENT = {
  id: "77",
  time: AT,
  kind: "consumer.deleted",
  type: "CONSUMER.DELETED",
  summary: "analytics-aggregator on order-archive",
  consumer: "analytics-aggregator",
  stream: "order-archive",
  server: "",
  clientName: "",
} as LensEvent;
const page = <T,>(items: T[]) => ({ items, nextPageToken: "", totalCount: items.length });

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(lens.getShellStatus).mockImplementation((async () =>
    create(GetShellStatusResponseSchema, { systems: [{ system: { id: "sys-a", name: "A" } }], defaultSystem: "sys-a", systemKnown: true })) as never);
  m.listServers.mockResolvedValue({ servers: [{ name: "nats-1", cluster: "c" }], expected: [], missing: [] } as never);
  m.listStreams.mockResolvedValue({ streams: [{ entity: { account: "APP", stream: "order-archive" } }] } as never);
  m.listConsumers.mockResolvedValue(
    Object.assign([{ entity: { account: "APP", stream: "order-archive", consumer: "analytics-aggregator" } }], { totalCount: 1, nextPageToken: "" }) as never,
  );
  m.listFindings.mockResolvedValue(page([FINDING]));
  m.listEvents.mockResolvedValue(page([EVENT]));
});

afterEach(() => vi.useRealTimers());

const box = () => screen.getByRole("combobox", { name: "Search this system" });
const settle = () => act(() => new Promise((r) => setTimeout(r, SEARCH_DEBOUNCE_MS + 100)));
const groupLabels = () => screen.queryAllByRole("group").map((g) => g.getAttribute("aria-labelledby") && document.getElementById(g.getAttribute("aria-labelledby")!)?.firstChild?.textContent);

describe("command bar", () => {
  it("shows a hint of the prefixes while the box is empty", async () => {
    renderApp(<SearchJump />, "/?system=sys-a");
    await userEvent.click(box());
    expect(screen.getByText(/Narrow with/)).toHaveTextContent("server:stream:consumer:kv:object:account:finding:event: since:2h");
    expect(box()).toHaveAttribute("aria-expanded", "false");
  });

  it("asks for no findings or events for one character", async () => {
    renderApp(<SearchJump />, "/?system=sys-a");
    await userEvent.type(box(), "a");
    await settle();
    expect(m.listFindings).not.toHaveBeenCalled();
    expect(m.listEvents).not.toHaveBeenCalled();
  });

  it("asks once for findings and once for events while ten characters are typed", async () => {
    renderApp(<SearchJump />, "/?system=sys-a");
    await userEvent.type(box(), "analytics-");
    await settle();
    await screen.findByText("consumer-010");
    expect(m.listFindings).toHaveBeenCalledTimes(1);
    expect(m.listEvents).toHaveBeenCalledTimes(1);
    expect(m.listFindings.mock.calls[0][2]).toEqual({ text: "analytics-", includeMuted: true, pageSize: 5 });
    expect(m.listEvents.mock.calls[0][2]).toEqual({ text: "analytics-", pageSize: 5 });
  });

  it("groups names, findings and events in order, and moves across the groups with the arrow keys", async () => {
    renderApp(<SearchJump />, "/?system=sys-a");
    await userEvent.type(box(), "analytics");
    await screen.findByText("consumer-010");
    await screen.findByText("CONSUMER.DELETED");
    expect(groupLabels()).toEqual(["Consumers", "Findings", "Events"]);
    const options = screen.getAllByRole("option");
    expect(options.map((o) => o.textContent)).toEqual([
      "Consumeranalytics-aggregatororder-archive",
      `Findingconsumer-010analytics-aggregatorWarning · firing since ${hm(AT)}`,
      `Eventanalytics-aggregatorCONSUMER.DELETED${hm(AT)}`,
    ]);
    expect(box()).toHaveAttribute("aria-activedescendant", options[0].id);
    await userEvent.keyboard("{ArrowDown}");
    expect(box()).toHaveAttribute("aria-activedescendant", options[1].id);
    await userEvent.keyboard("{ArrowDown}{ArrowDown}");
    expect(box()).toHaveAttribute("aria-activedescendant", options[0].id);
    await userEvent.keyboard("{ArrowUp}");
    expect(box()).toHaveAttribute("aria-activedescendant", options[2].id);
  });

  it("opens a finding's page on Enter", async () => {
    renderApp(<SearchJump />, "/?system=sys-a");
    await userEvent.type(box(), "consumer-010");
    await screen.findByText(`Warning · firing since ${hm(AT)}`);
    await userEvent.keyboard("{ArrowDown}{ArrowUp}{Enter}");
    expect(screen.getByTestId("location")).toHaveTextContent("/findings/consumer-010%3AAPP%2Forder-archive%2Fanalytics-aggregator?system=sys-a");
    expect(box()).toHaveValue("");
  });

  it("opens an event on the events page", async () => {
    renderApp(<SearchJump />, "/?system=sys-a");
    await userEvent.type(box(), "event: deleted");
    await userEvent.click(await screen.findByText("CONSUMER.DELETED"));
    expect(screen.getByTestId("location")).toHaveTextContent("/events?id=77&system=sys-a");
  });

  it("narrows to one kind with a prefix, shows it as a chip, and drops it with the chip", async () => {
    renderApp(<SearchJump />, "/?system=sys-a");
    await userEvent.type(box(), "event: since:2h");
    await screen.findByText("CONSUMER.DELETED");
    expect(groupLabels()).toEqual(["Events"]);
    expect(m.listFindings).not.toHaveBeenCalled();
    const [, spec, filter] = m.listEvents.mock.calls.at(-1)!;
    expect(filter).toEqual({ text: "", pageSize: 10 });
    expect(spec.kind === "fixed" && spec.to - spec.from).toBe(2 * 3_600_000);
    expect(screen.getByText("Events", { selector: ".search-chip" })).toBeInTheDocument();
    expect(screen.getByText("Since 2h", { selector: ".search-chip" })).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Remove since 2h" }));
    expect(box()).toHaveValue("event:");
    expect(box()).toHaveFocus();
  });

  it("narrows to a group from its show all row", async () => {
    m.listFindings.mockResolvedValue({ items: [FINDING], nextPageToken: "x", totalCount: 9 });
    renderApp(<SearchJump />, "/?system=sys-a");
    await userEvent.type(box(), "consumer-010");
    const more = await screen.findByRole("option", { name: "Show all 9" });
    await userEvent.click(more);
    expect(box()).toHaveValue("finding: consumer-010");
    await settle();
    await waitFor(() => expect(m.listFindings.mock.calls.at(-1)![2]).toEqual({ text: "consumer-010", includeMuted: true, pageSize: 10 }));
    expect(await screen.findByRole("option", { name: "Open the findings page" })).toBeInTheDocument();
  });

  it("cancels the request of a text that changed, and never shows its result", async () => {
    const signals: AbortSignal[] = [];
    m.listFindings.mockImplementation((_s, _r, f, signal) => {
      signals.push(signal!);
      if (f.text === "orde") return new Promise(() => {});
      return Promise.resolve(page([{ ...FINDING, checkCode: `match-${f.text}` }]));
    });
    renderApp(<SearchJump />, "/?system=sys-a");
    await userEvent.type(box(), "orde");
    await settle();
    expect(signals).toHaveLength(1);
    await userEvent.type(box(), "r");
    await settle();
    await screen.findByText("match-order");
    expect(signals).toHaveLength(2);
    expect(signals[0].aborted).toBe(true);
    expect(signals[1].aborted).toBe(false);
  });

  it("keeps the name results while findings and events load or fail", async () => {
    m.listFindings.mockImplementation(() => new Promise(() => {}));
    m.listEvents.mockRejectedValue(new Error("down"));
    renderApp(<SearchJump />, "/?system=sys-a");
    await userEvent.type(box(), "analytics");
    await screen.findByText("Could not search events");
    expect(screen.getByText("Searching…")).toBeInTheDocument();
    expect(screen.getAllByRole("option").map((o) => o.textContent)).toEqual(["Consumeranalytics-aggregatororder-archive"]);
  });
});
