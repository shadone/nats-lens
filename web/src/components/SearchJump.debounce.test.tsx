// The command bar's debounce, on fake timers: the other command bar tests
// hold it (see SearchJump.command.test.tsx), so this file checks that the
// remote search waits SEARCH_DEBOUNCE_MS after the last change.
import { create } from "@bufbuild/protobuf";
import { act, fireEvent, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { lens } from "../api/client";
import * as events from "../api/events";
import * as requests from "../api/requests";
import { GetShellStatusResponseSchema } from "../gen/natslens/v1/systems_pb";
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
}));
vi.mock("../api/events", async (orig) => ({
  ...(await orig<typeof import("../api/events")>()),
  searchEvents: vi.fn(),
}));
const m = vi.mocked(requests);
const ev = vi.mocked(events);
const empty = { items: [], nextPageToken: "", totalCount: 0 };

beforeEach(() => {
  vi.useFakeTimers();
  vi.mocked(lens.getShellStatus).mockImplementation((async () =>
    create(GetShellStatusResponseSchema, { systems: [{ system: { id: "sys-a", name: "A" } }], defaultSystem: "sys-a", systemKnown: true })) as never);
  m.listServers.mockResolvedValue({ servers: [], expected: [], missing: [] } as never);
  m.listStreams.mockResolvedValue({ streams: [] } as never);
  m.listConsumers.mockResolvedValue(Object.assign([], { totalCount: 0, nextPageToken: "" }) as never);
  m.listFindings.mockResolvedValue(empty as never);
  ev.searchEvents.mockResolvedValue(empty as never);
});

afterEach(() => vi.useRealTimers());

const advance = (ms: number) => act(async () => { await vi.advanceTimersByTimeAsync(ms); });

it("asks for findings once, for the text that rested SEARCH_DEBOUNCE_MS", async () => {
  renderApp(<SearchJump />, "/?system=sys-a");
  const box = screen.getByRole("combobox", { name: "Search this system" });
  fireEvent.focus(box);
  const text = "analytics";
  for (let n = 1; n <= text.length; n++) {
    fireEvent.change(box, { target: { value: text.slice(0, n) } });
    await advance(SEARCH_DEBOUNCE_MS - 50);
  }
  expect(m.listFindings).not.toHaveBeenCalled();
  await advance(SEARCH_DEBOUNCE_MS);
  expect(m.listFindings).toHaveBeenCalledTimes(1);
  expect(m.listFindings.mock.calls[0][2]).toMatchObject({ text });
});
