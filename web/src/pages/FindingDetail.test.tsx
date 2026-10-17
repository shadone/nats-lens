import { act, screen } from "@testing-library/react";
import { Route, Routes } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Finding } from "../api/model";
import * as requests from "../api/requests";
import { renderApp } from "../test/render";
import { FindingDetail, atHistoryStart } from "./FindingDetail";

vi.mock("../api/requests", () => ({ getFinding: vi.fn(), getTimeline: vi.fn(), getLensInfo: vi.fn(), muteFinding: vi.fn(), unmuteFinding: vi.fn() }));
const m = vi.mocked(requests);

const base: Finding = {
  id: "stream-020:A/s",
  checkCode: "stream-020",
  checkName: "Backlog growing",
  severity: "warning",
  state: "firing",
  since: Date.now() - 3_600_000,
  detail: "",
  remediation: "",
  relatedConfig: [],
  relatedConfigSource: "",
  transitionsLastHour: 1,
};

describe("FindingDetail", () => {
  afterEach(() => vi.useRealTimers());

  it("refreshes in live mode and shows a new state", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    m.getTimeline.mockResolvedValue({ start: 0, bucketMs: 900_000, bucketCount: 0, rows: [], kubernetes: [], nats: [], windows: [], natsTruncated: false, findingTruncated: false, kubernetesTruncated: false });
    m.getFinding.mockResolvedValueOnce({ finding: base, transitions: [] });
    m.getFinding.mockResolvedValue({ finding: { ...base, state: "resolved", resolvedAt: Date.now() }, transitions: [] });
    renderApp(
      <Routes>
        <Route path="/findings/:id" element={<FindingDetail />} />
      </Routes>,
      "/findings/stream-020%3AA%2Fs",
    );
    expect(await screen.findByText("First seen by nats-lens")).toBeInTheDocument();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(31_000);
    });
    expect(await screen.findByText("RESOLVED")).toBeInTheDocument();
    expect(m.getFinding).toHaveBeenCalledTimes(2);
  });
});

describe("FindingDetail times", () => {
  it("shows when the condition began apart from when nats-lens first saw it", async () => {
    const began = Date.UTC(2026, 9, 15, 9, 0);
    m.getTimeline.mockResolvedValue({ start: 0, bucketMs: 900_000, bucketCount: 0, rows: [], kubernetes: [], nats: [], windows: [], natsTruncated: false, findingTruncated: false, kubernetesTruncated: false });
    m.getFinding.mockResolvedValue({ finding: { ...base, conditionSince: began }, transitions: [] });
    renderApp(
      <Routes>
        <Route path="/findings/:id" element={<FindingDetail />} />
      </Routes>,
      "/findings/stream-020%3AA%2Fs",
    );
    expect(await screen.findByText("First seen by nats-lens")).toBeInTheDocument();
    expect(screen.getByText("Condition since")).toBeInTheDocument();
    expect(screen.getByText(/2026-10-15 09:00/)).toBeInTheDocument();
  });
});

describe("the history start note", () => {
  const h = Date.UTC(2026, 9, 17, 9, 0);
  it("marks a firing finding first seen at the start of the history", () => {
    expect(atHistoryStart({ ...base, since: h + 20_000 }, h)).toBe(true);
  });
  it("leaves out resolved findings, findings seen later, and an unknown start", () => {
    expect(atHistoryStart({ ...base, since: h + 20_000, state: "resolved" }, h)).toBe(false);
    expect(atHistoryStart({ ...base, since: h + 3_600_000 }, h)).toBe(false);
    expect(atHistoryStart({ ...base, since: h - 1 }, h)).toBe(false);
    expect(atHistoryStart({ ...base, since: h }, undefined)).toBe(false);
  });
  it("shows the note on the page", async () => {
    m.getLensInfo.mockResolvedValue({ identityMode: "request", version: "", cluster: "", collectIntervalMs: 30_000, retentionMs: 0, live: true, demo: false, historyStart: h });
    m.getTimeline.mockResolvedValue({ start: 0, bucketMs: 900_000, bucketCount: 0, rows: [], kubernetes: [], nats: [], windows: [], natsTruncated: false, findingTruncated: false, kubernetesTruncated: false });
    m.getFinding.mockResolvedValue({ finding: { ...base, since: h + 20_000 }, transitions: [] });
    renderApp(
      <Routes>
        <Route path="/findings/:id" element={<FindingDetail />} />
      </Routes>,
      "/findings/stream-020%3AA%2Fs",
    );
    expect(await screen.findByText(/first seen when nats-lens history begins/)).toBeInTheDocument();
  });
});
