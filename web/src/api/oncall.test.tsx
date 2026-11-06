import { create } from "@bufbuild/protobuf";
import { timestampFromMs, durationMs } from "@bufbuild/protobuf/wkt";
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { beforeEach, expect, it, vi } from "vitest";
import { GetOnCallResponseSchema } from "../gen/natslens/v1/oncall_pb";
import { FindingSchema, FindingTallySchema } from "../gen/natslens/v1/findings_pb";
import { AcknowledgementSchema, AcknowledgeFindingResponseSchema, EndAcknowledgementResponseSchema } from "../gen/natslens/v1/acknowledgements_pb";
import { ack, onCallCard, onCallFinding } from "../test/fixtures/oncall";
import { onCall } from "./oncall";
import { finding, tally } from "./adapt-findings";
import { lens } from "./client";
import { acknowledgeFinding, endAcknowledgement, getOnCall } from "./requests";
import { useSaveAcknowledgement } from "./queries";
vi.mock("./client", () => ({ lens: { acknowledgeFinding: vi.fn(), endAcknowledgement: vi.fn(), getOnCall: vi.fn() } }));
const m = vi.mocked(lens);
const protoAck = () => create(AcknowledgementSchema, { ...ack, createdAt: timestampFromMs(ack.createdAt!), quietUntil: timestampFromMs(ack.quietUntil!) });
beforeEach(() => { vi.resetAllMocks(); m.acknowledgeFinding.mockResolvedValue(create(AcknowledgeFindingResponseSchema, { acknowledgement: protoAck() })); });
it("adapts optional zeros, false, unset facts, episode acknowledgement and tally", () => {
  const unset = onCall(create(GetOnCallResponseSchema)); expect(unset.now).toEqual({});
  const d = onCall(create(GetOnCallResponseSchema, { now: { serversAnswering: 0, deliveredPerSecond: 0 }, assets: [{ online: 0, answering: false }] }));
  expect(d.now).toMatchObject({ serversAnswering: 0, deliveredPerSecond: 0 }); expect(d.now.serversExpected).toBeUndefined(); expect(d.assets[0]).toMatchObject({ online: 0, answering: false }); expect(d.assets[0].replicas).toBeUndefined();
  expect(finding(create(FindingSchema, { acknowledgement: protoAck() })).acknowledgement).toEqual(ack);
  expect(tally(create(FindingTallySchema, { acknowledged: 2, critical: 3 }))).toMatchObject({ critical: 3, acknowledged: 2 });
});
it("sends read and write requests to the explicit system with exact duration or until-resolved", async () => {
  m.getOnCall.mockResolvedValue(create(GetOnCallResponseSchema)); await getOnCall("example-staging", "id");
  expect(m.getOnCall).toHaveBeenCalledWith({ system: "example-staging", findingId: "id" }, { signal: undefined });
  await acknowledgeFinding("example-prod", { findingId: "id", note: "n", ackedBy: "alice", quietMs: 7_200_000 });
  const req = m.acknowledgeFinding.mock.calls[0][0]; expect(req).toMatchObject({ system: "example-prod", findingId: "id", ackedBy: "alice", untilResolved: false }); expect(durationMs(req.quietFor! as Parameters<typeof durationMs>[0])).toBe(7_200_000);
  await acknowledgeFinding("example-prod", { findingId: "id", note: "", ackedBy: "", untilResolved: true }); expect(m.acknowledgeFinding.mock.calls[1][0].quietFor).toBeUndefined();
  m.endAcknowledgement.mockResolvedValue(create(EndAcknowledgementResponseSchema, { acknowledgement: { ...protoAck(), endedBy: "bob", endedAt: timestampFromMs(ack.quietUntil!), endReason: "released" } }));
  expect(await endAcknowledgement("example-prod", "a1", "bob")).toMatchObject({ endedBy: "bob", endReason: "released" });
});
it("patches both current caches then invalidates only the mutation's system for replace and release", async () => {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  const key = (kind: string, sys = "example-prod") => [kind, sys, onCallFinding.id];
  for (const kind of ["finding", "oncall"]) for (const sys of ["example-prod", "example-global"]) qc.setQueryData(key(kind, sys), onCallCard);
  qc.setQueryData(["overview", "example-prod"], {}); qc.setQueryData(["overview", "example-global"], {});
  const { result } = renderHook(useSaveAcknowledgement, { wrapper: ({ children }: { children: ReactNode }) => <QueryClientProvider client={qc}>{children}</QueryClientProvider> });
  await act(async () => { await result.current.mutateAsync({ system: "example-prod", action: "ack", input: { findingId: onCallFinding.id, note: "", ackedBy: "alice", quietMs: 7_200_000 } }); });
  for (const kind of ["finding", "oncall"]) {
    expect(qc.getQueryData<typeof onCallCard>(key(kind))?.finding?.acknowledgement).toEqual(ack);
    expect(qc.getQueryData<typeof onCallCard>(key(kind, "example-global"))?.finding?.acknowledgement).toBeUndefined();
  }
  expect(qc.getQueryState(["overview", "example-prod"])?.isInvalidated).toBe(true); expect(qc.getQueryState(["overview", "example-global"])?.isInvalidated).toBe(false);
  m.endAcknowledgement.mockResolvedValue(create(EndAcknowledgementResponseSchema, { acknowledgement: { ...protoAck(), endedAt: timestampFromMs(ack.quietUntil!), endReason: "released", endedBy: "alice" } }));
  await act(async () => { await result.current.mutateAsync({ system: "example-prod", action: "release", id: ack.id, endedBy: "alice" }); });
  await waitFor(() => expect(qc.getQueryData<typeof onCallCard>(key("finding"))?.finding?.acknowledgement).toBeUndefined());
});
