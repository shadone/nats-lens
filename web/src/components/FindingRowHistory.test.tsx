// A findings row of a finding already true when the history begins says so
// in place of a first-seen time, which would read as fresh.
import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Finding } from "../api/model";
import { renderApp } from "../test/render";
import { FindingRow } from "./FindingParts";

const END = Date.parse("2026-11-05T22:47:18Z");
const START = END - 9 * 3_600_000;

const f: Finding = {
  id: "stream-004:APP/order-jobs",
  checkCode: "stream-004",
  checkName: "Stream near its message or byte limit",
  severity: "warning",
  state: "firing",
  entity: { kind: "stream", key: "APP/order-jobs", name: "order-jobs", account: "APP", stream: "order-jobs", consumer: "", server: "" },
  since: START + 20_000,
  detail: "",
  remediation: "",
  relatedConfig: [],
  relatedConfigSource: "",
  transitionsLastHour: 0,
};

describe("FindingRow at the history start", () => {
  it("says already true when the history begins, with when it begins", () => {
    renderApp(<FindingRow f={f} end={END} historyStart={START} />);
    const mark = screen.getByText(/already true/);
    expect(mark).toHaveTextContent("already truewhen the history begins");
    expect(mark).toHaveAttribute("title", "Already true when the history begins, at 13:47 UTC: nats-lens cannot know when it started.");
    expect(screen.queryByText(/^since /)).toBeNull();
  });

  it("keeps the first-seen time for a finding that began later, or without a history start", () => {
    renderApp(<FindingRow f={{ ...f, since: START + 3_600_000 }} end={END} historyStart={START} />);
    expect(screen.getByText(/^since /)).toBeInTheDocument();
    renderApp(<FindingRow f={f} end={END} />);
    expect(screen.queryAllByText(/already true/)).toHaveLength(0);
  });
});
