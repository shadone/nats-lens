import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { screen } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { lens } from "../api/client";
import { GetSlowConsumerInvestigationResponseSchema } from "../gen/natslens/v1/slow_consumers_pb";
import { renderApp } from "../test/render";
import { SlowConsumerInvestigation } from "./SlowConsumerInvestigation";

vi.mock("../api/client", () => ({ lens: { getSlowConsumerInvestigation: vi.fn() } }));
const stamp = (s: string) => timestampFromDate(new Date(`2026-10-17T${s}Z`));
const response = () => create(GetSlowConsumerInvestigationResponseSchema, {
  range: { start: stamp("13:30:00"), end: stamp("14:30:00") }, detections: 5n, recordedDisconnects: 2n,
  attributionLimit: "Counter time and connection are unknown. Events are separate evidence.",
  observations: [{ server: "nats-0", serverId: "PROCESS0", epoch: 2n, intervalStart: stamp("13:40:00"), intervalEnd: stamp("13:40:30"), increase: 5n }],
  events: [1,2].map(id => ({ event: { id: String(id), time: stamp("13:40:20"), clientName: "checkout@one", server: "nats-0", reason: "Slow Consumer (Write Deadline)" }, serverId: "PROCESS0", connectionId: "12" })),
  findings: [{ findingId: "slow-1", checkCode: "server-009", time: stamp("13:40:30"), entity: { key: "nats-0" }, detail: "5 detections" }],
});
beforeEach(() => vi.resetAllMocks());
it("shows interval and payload identities separately, preserves exact scope on evidence links", async () => {
  vi.mocked(lens.getSlowConsumerInvestigation).mockResolvedValue(response());
  renderApp(<SlowConsumerInvestigation />, "/servers/slow-consumers?from=2026-10-17T13:30:00Z&to=2026-10-17T14:30:00Z&system=example-prod&account=SHOP");
  expect(await screen.findByText("5 counter detections")).toBeInTheDocument();
  expect(screen.getByText("2 recorded slow-consumer disconnects")).toBeInTheDocument();
  expect(screen.getByText("2026-10-17T13:40:00Z to 2026-10-17T13:40:30Z")).toBeInTheDocument();
  expect(screen.getAllByText("Process PROCESS0 · Connection 12 (recorded ID)")).toHaveLength(2);
  const event = new URL(screen.getByRole("link", { name: "Open recorded event 1" }).getAttribute("href")!, "http://example.com");
  expect(Object.fromEntries(event.searchParams)).toEqual({ from: "2026-10-17T13:30:00Z", to: "2026-10-17T14:30:00Z", system: "example-prod", account: "SHOP", id: "1" });
  const interval = new URL(screen.getByRole("link", { name: "Investigate this interval" }).getAttribute("href")!, "http://example.com");
  expect(interval.searchParams.get("server")).toBe("nats-0");
  expect(interval.searchParams.get("from")).toBe("2026-10-17T13:40:00Z");
  expect(lens.getSlowConsumerInvestigation).toHaveBeenCalledWith(expect.objectContaining({system:"example-prod",account:"SHOP",limit:100}),expect.objectContaining({signal:expect.any(AbortSignal)}));
});
it.each([false,true])("distinguishes unavailable capture from no matching recorded disconnects (captured %s)",async captured => {
 const r=response();r.events=[];r.recordedDisconnects=0n;r.findings=[];r.eventsCaptured=captured;r.captureGaps=false;
 vi.mocked(lens.getSlowConsumerInvestigation).mockResolvedValue(r);
 renderApp(<SlowConsumerInvestigation />,"/servers/slow-consumers");
 expect(await screen.findByText(/nats-lens cannot identify the affected connection/)).toBeInTheDocument();
 expect(screen.getByRole("note")).toHaveTextContent(captured ? "No known event-capture gaps" : "Event capture history is unavailable");
 expect(screen.queryByRole("link",{name:/Open recorded event/})).not.toBeInTheDocument();
});
it("makes incomplete history and bounded results explicit",async()=>{
 const r=response();r.eventsCaptured=true;r.captureGaps=true;r.eventsStart=stamp("13:35:00");r.observationsTruncated=true;r.eventsTruncated=true;r.findingsTruncated=true;
 vi.mocked(lens.getSlowConsumerInvestigation).mockResolvedValue(r);
 renderApp(<SlowConsumerInvestigation />,"/servers/slow-consumers");
 expect(await screen.findByText(/Event history is incomplete/)).toBeInTheDocument();
 expect(screen.getByText(/Observation list limited/)).toBeInTheDocument();
 expect(screen.getByText(/Event list limited/)).toBeInTheDocument();
 expect(screen.getByText(/Finding list limited/)).toBeInTheDocument();
});
