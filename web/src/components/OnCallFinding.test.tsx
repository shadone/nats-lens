import { act, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Route, Routes } from "react-router";
import { beforeEach, expect, it, vi } from "vitest";
import * as requests from "../api/requests";
import { renderApp } from "../test/render";
import { ack, onCallCard, onCallFinding } from "../test/fixtures/oncall";
import { FindingDetail } from "../pages/FindingDetail";
import { AcknowledgementMark } from "./Acknowledgement";
import { CauseGroups } from "./CauseGroups";
import { FindingRow } from "./FindingParts";
import { StatusLine } from "./StatusLine";
vi.mock("../api/requests", async orig => ({ ...(await orig<typeof import("../api/requests")>()), getOnCall: vi.fn(), getFinding: vi.fn(), getTimeline: vi.fn(), getLensInfo: vi.fn(), listFindings: vi.fn(), acknowledgeFinding: vi.fn() }));
const m = vi.mocked(requests); const getOnCall = vi.mocked(requests.getOnCall);
let change: () => void; let phone: boolean;
beforeEach(() => {
  vi.resetAllMocks(); localStorage.clear(); phone = true;
  vi.stubGlobal("matchMedia", vi.fn((query: string) => ({ get matches() { return phone; }, media: query, addEventListener: (_: string, cb: () => void) => { change = cb; }, removeEventListener: vi.fn() })));
  getOnCall.mockResolvedValue(onCallCard);
  m.getFinding.mockResolvedValue({ finding: onCallFinding, transitions: [] });
  m.getLensInfo.mockResolvedValue({ identityMode: "request", version: "", cluster: "example-prod", historyStart: onCallFinding.since, collectIntervalMs: 30_000, retentionMs: 0, live: true, demo: true });
  m.getTimeline.mockResolvedValue({ rows: [], start: 0, bucketMs: 0, bucketCount: 0, kubernetes: [], nats: [], windows: [], natsTruncated: false, findingTruncated: false, kubernetesTruncated: false });
  m.listFindings.mockResolvedValue({ items: [], nextPageToken: "", totalCount: 0 });
});
function show() { return renderApp(<Routes><Route path="/findings/:id" element={<FindingDetail />} /></Routes>, `/findings/${encodeURIComponent(onCallFinding.id)}?system=example-prod`); }
it("selects the phone tree at max760 and loads desktop sections only on demand", async () => {
  show(); expect(await screen.findByRole("heading", { name: "Right now" })).toBeInTheDocument();
  expect(window.matchMedia).toHaveBeenCalledWith("(max-width: 760px)");
  expect(m.getFinding).not.toHaveBeenCalled(); expect(screen.queryByText("Status timeline")).toBeNull();
  expect(screen.getByText("Present observations are not reported.")).toBeInTheDocument();
  expect(screen.getByText("Before stored history.")).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "already true when nats-lens started" })).toHaveAttribute("href", expect.stringContaining("/data-history"));
  const replay = new URL(screen.getByRole("link", { name: "Open in Replay" }).getAttribute("href")!, "http://x");
  expect(Date.parse(replay.searchParams.get("a")!)).toBe(onCallCard.storyA);
  expect(Date.parse(replay.searchParams.get("b")!)).toBe(onCallCard.storyB);
  await userEvent.click(screen.getByText("All details")); expect(await screen.findByText("Status timeline")).toBeInTheDocument();
  await act(async () => { phone = false; change(); });
  expect(screen.queryByRole("heading", { name: "Right now" })).toBeNull();
  expect(screen.getByRole("button", { name: "Acknowledge…" })).toBeInTheDocument();
});
it("shows saved acknowledgement and moves focus before its slow refetch finishes", async () => {
  m.acknowledgeFinding.mockResolvedValue(ack); localStorage.setItem("nats-lens-name", "alice");
  show(); await userEvent.click(await screen.findByRole("button", { name: "I'm on it" }));
  getOnCall.mockImplementation(() => new Promise(() => {}));
  await userEvent.click(screen.getByRole("button", { name: "Acknowledge" }));
  const section = await screen.findByRole("region", { name: "Acknowledgement" });
  expect(section).toHaveTextContent("Acknowledged by alice@example.com");
  await waitFor(() => expect(section).toHaveFocus());
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(screen.getByRole("button", { name: "Update…" })).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Hand back…" })).toBeInTheDocument();
});
it("renders marks in cause cards and rows without changing history marks or tally severity", () => {
  const f = { ...onCallFinding, acknowledgement: ack };
  renderApp(<><CauseGroups groups={[{ key: "c", findings: [f], severity: "critical", checkCodes: [f.checkCode], parentCount: 1 }]} muted={[]} onMute={() => {}} /><FindingRow f={f} end={ack.createdAt!} /><StatusLine tally={{ critical: 1, warning: 0, info: 0, muted: 0, flapping: 0, resolved24h: 0, acknowledged: 1 }} live now={ack.createdAt!} /></>);
  expect(screen.getAllByText(/Acknowledged by alice@example.com/)).toHaveLength(2);
  expect(screen.getByText("1 acknowledged")).toBeInTheDocument();
  expect(screen.getByText("1 CRITICAL")).toBeInTheDocument();
});
it("stops saying quiet at expiry without removing ownership and describes releases/replacements", () => {
  const { rerender } = renderApp(<AcknowledgementMark acknowledgement={ack} now={ack.quietUntil! - 1} />);
  expect(screen.getByText(/quiet until 13:35 UTC/)).toBeInTheDocument();
  rerender(<AcknowledgementMark acknowledgement={ack} now={ack.quietUntil} />);
  expect(screen.getByText(/Acknowledged by alice@example.com/)).toBeInTheDocument(); expect(screen.queryByText(/quiet until/)).toBeNull();
  rerender(<AcknowledgementMark acknowledgement={{ ...ack, endedAt: ack.quietUntil, endedBy: "bob", endReason: "replaced" }} now={ack.quietUntil} />);
  expect(screen.getByText(/Replaced by bob/)).toBeInTheDocument();
  rerender(<AcknowledgementMark acknowledgement={{ ...ack, endedAt: ack.quietUntil, endedBy: "bob", endReason: "released" }} now={ack.quietUntil} />);
  expect(screen.getByText(/Handed back by bob/)).toBeInTheDocument();
});
it("keeps the desktop acknowledge action beside mute", async () => {
  phone = false; show();
  const mute = await screen.findByRole("button", { name: "Mute…" });
  expect(mute.parentElement).toContainElement(screen.getByRole("button", { name: "Acknowledge…" }));
});
it("uses a refetched resolve to end ownership rather than reviving a local saved acknowledgement", async () => {
  m.acknowledgeFinding.mockResolvedValue(ack); localStorage.setItem("nats-lens-name", "alice");
  show(); await userEvent.click(await screen.findByRole("button", { name: "I'm on it" }));
  getOnCall.mockResolvedValue({ ...onCallCard, finding: { ...onCallFinding, state: "resolved", resolvedAt: Date.now() } });
  await userEvent.click(screen.getByRole("button", { name: "Acknowledge" }));
  await screen.findByText(/Resolved.*Present observations/);
  expect(screen.queryByRole("button", { name: "Update…" })).toBeNull();
});
