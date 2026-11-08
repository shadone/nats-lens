import { act, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import * as requests from "../api/requests";
import { renderApp } from "../test/render";
import { Consumers } from "./Consumers";

vi.mock("../api/requests", () => ({ listConsumers: vi.fn(), listStreams: vi.fn(), getLensInfo: vi.fn(() => new Promise(() => {})) }));

const entity = (stream: string) => ({ kind: "stream", key: `APP/${stream}`, name: stream, account: "APP", accountName: "APP", stream, consumer: "", server: "" });

describe("Consumers stream filter", () => {
  afterEach(() => vi.useRealTimers());

  it("fills the stream filter once and does not poll it, while the consumers follow the live range", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    vi.mocked(requests.listConsumers).mockResolvedValue(Object.assign([], { totalCount: 0, nextPageToken: "" }));
    vi.mocked(requests.listStreams).mockResolvedValue({
      streams: [{ entity: entity("order-jobs") }, { entity: entity("order-archive") }],
      totals: { bytes: 0, durableConsumers: 0, ephemeralConsumers: 0 },
    } as never);
    renderApp(<Consumers />, "/consumers");
    const select = screen.getByRole("combobox", { name: "Stream" });
    await waitFor(() => expect(within(select).getAllByRole("option").map((o) => o.textContent)).toEqual(["All streams", "order-jobs (APP)", "order-archive (APP)"]));
    await waitFor(() => expect(requests.listConsumers).toHaveBeenCalledTimes(1));
    expect(requests.listStreams).toHaveBeenCalledTimes(1);
    await act(() => vi.advanceTimersByTimeAsync(31_000));
    await act(() => vi.advanceTimersByTimeAsync(31_000));
    expect(vi.mocked(requests.listConsumers).mock.calls.length).toBeGreaterThanOrEqual(3);
    expect(requests.listStreams).toHaveBeenCalledTimes(1);
  });
});
