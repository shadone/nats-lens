import { create } from "@bufbuild/protobuf";
import { durationFromMs, timestampFromMs } from "@bufbuild/protobuf/wkt";
import { describe, expect, it } from "vitest";
import { GetDataHistoryResponseSchema, HistoryGapKind } from "../gen/natslens/v1/history_pb";
import { dataHistory, isHatched } from "./history";

const AT = Date.parse("2026-11-05T22:47:18Z");
const RANGE = { start: AT - 86_400_000, end: AT };

describe("dataHistory", () => {
  it("keeps unset values undefined, never 0", () => {
    const d = dataHistory(create(GetDataHistoryResponseSchema, {}), RANGE);
    expect(d.rangeStart).toBe(RANGE.start);
    expect(d.rangeEnd).toBe(RANGE.end);
    expect(d.historyStart).toBeUndefined();
    expect(d.retention).toBeUndefined();
    expect(d.coverage.from).toBeUndefined();
    expect(d.coverage.withoutData).toBeUndefined();
    expect(d.archive.enabled).toBe(false);
    expect(d.archive.hours).toBeUndefined();
    expect(d.gaps).toEqual([]);
  });

  it("reads the gaps, an open one without an end and its missing collection unset", () => {
    const d = dataHistory(
      create(GetDataHistoryResponseSchema, {
        historyStart: timestampFromMs(AT - 9 * 3_600_000),
        retention: durationFromMs(14 * 86_400_000),
        collectInterval: durationFromMs(30_000),
        coverage: { coveredFrom: timestampFromMs(AT - 9 * 3_600_000), coveredTo: timestampFromMs(AT), epochs: 715n, expectedEpochs: 1080n, withoutData: durationFromMs(0) },
        gaps: [
          { kind: HistoryGapKind.NOT_CONNECTED, start: timestampFromMs(AT - 3_600_000), beforeEpoch: 720n, beforeEpochAt: timestampFromMs(AT - 3_600_000), eventId: "29" },
          { kind: HistoryGapKind.EVENTS_NOT_CAPTURED, start: timestampFromMs(AT - 7_200_000), end: timestampFromMs(AT - 7_150_000), eventId: "8", connection: "sys" },
          { kind: HistoryGapKind.BEFORE_HISTORY, start: timestampFromMs(RANGE.start), end: timestampFromMs(AT - 9 * 3_600_000) },
        ],
        gapsTotal: 2,
        listedGapsTotal: 1,
        archive: { enabled: true, location: "/var/lib/natslens/archive", hours: 0, listedAt: timestampFromMs(AT) },
        uses: [{ id: "accept_review", needs: durationFromMs(694_800_000), has: durationFromMs(32_400_000), enoughAt: timestampFromMs(AT + 86_400_000) }],
        findingsAtStart: 1,
      }),
      RANGE,
    );
    expect(d.intervalMs).toBe(30_000);
    expect(d.coverage).toEqual({ from: AT - 9 * 3_600_000, to: AT, epochs: 715, expected: 1080, partial: 0, withoutData: 0 });
    expect(d.gaps[0]).toEqual({ kind: "notConnected", start: AT - 3_600_000, end: undefined, beforeEpoch: "720", beforeEpochAt: AT - 3_600_000, afterEpoch: undefined, afterEpochAt: undefined, eventId: "29", connection: "" });
    expect(d.gaps.map((g) => [g.kind, isHatched(g)])).toEqual([
      ["notConnected", true],
      ["eventsNotCaptured", false],
      ["beforeHistory", true],
    ]);
    expect(d.archive.hours).toBe(0);
    expect(d.uses[0]).toEqual({ id: "accept_review", checkCode: "", needs: 694_800_000, has: 32_400_000, enough: false, enoughAt: AT + 86_400_000, never: false });
    expect(d.findingsAtStart).toBe(1);
  });
});
