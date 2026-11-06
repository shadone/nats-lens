// Data history view models for the page's tests: example-staging's story
// (9 hours of history, nats-lens restarted 3 minutes, its connection
// closed and still open) and example-prod's (30 days, an archive).
import type { ArchiveState, DataHistory, HistoryGap, HistoryUse } from "../../api/history";

export const NOW = Date.parse("2026-11-05T22:47:18Z");
const MIN = 60_000;
const HOUR = 60 * MIN;
const DAY = 24 * HOUR;

export function gap(over: Partial<HistoryGap> = {}): HistoryGap {
  return { kind: "noCollections", eventId: "", connection: "", ...over };
}

export function use(over: Partial<HistoryUse> = {}): HistoryUse {
  return { id: "growth", checkCode: "", needs: 6 * HOUR, has: 9 * HOUR, enough: true, never: false, ...over };
}

export const NO_ARCHIVE: ArchiveState = { enabled: false, location: "", newestFinal: false, lastError: "" };

export function staging(): DataHistory {
  const start = NOW - 9 * HOUR;
  const sampleSpan = 6 * HOUR - 4000; // stops three hours and four seconds before NOW
  return {
    rangeStart: NOW - DAY,
    rangeEnd: NOW,
    historyStart: start,
    newestEpochAt: NOW - 3 * HOUR - 4000,
    eventsStart: start + 3 * MIN,
    retention: 14 * DAY,
    retentionFrom: NOW - 14 * DAY,
    intervalMs: 30_000,
    coverage: { from: start, to: NOW, epochs: 715, expected: 1080, partial: 0, withoutData: 10_984_000 },
    gaps: [
      gap({ kind: "eventsNotCaptured", start: NOW - 3 * HOUR, eventId: "28", connection: "sys" }),
      gap({ kind: "notConnected", start: NOW - 3 * HOUR - 4000, beforeEpoch: "720", beforeEpochAt: NOW - 3 * HOUR - 4000, eventId: "29" }),
      gap({
        kind: "notRunning",
        start: Date.parse("2026-11-05T17:50:44Z"),
        end: Date.parse("2026-11-05T17:53:44Z"),
        beforeEpoch: "487",
        beforeEpochAt: Date.parse("2026-11-05T17:50:44Z"),
        afterEpoch: "493",
        afterEpochAt: Date.parse("2026-11-05T17:53:44Z"),
        eventId: "18",
      }),
      gap({ kind: "eventsNotCaptured", start: Date.parse("2026-11-05T15:47:18Z"), end: Date.parse("2026-11-05T15:48:08Z"), eventId: "8", connection: "sys" }),
      gap({ kind: "beforeHistory", start: NOW - DAY, end: start }),
    ],
    gapsTotal: 3,
    listedGapsTotal: 2,
    archive: NO_ARCHIVE,
    uses: [
      use({ id: "growth", has: sampleSpan, enough: false }),
      use({ id: "forecast:server-014", checkCode: "server-014", has: sampleSpan, enough: false }),
      use({ id: "lookback:server-019", checkCode: "server-019", needs: DAY, has: sampleSpan, enough: false }),
      use({ id: "accept_review", needs: 8 * DAY + HOUR, has: sampleSpan, enough: false }),
    ],
    findingsAtStart: 1,
  };
}

export function prod(): DataHistory {
  const start = NOW - 30 * DAY;
  return {
    ...staging(),
    historyStart: start,
    eventsStart: NOW - DAY,
    retention: 30 * DAY,
    retentionFrom: start,
    coverage: { from: NOW - DAY, to: NOW, epochs: 2880, expected: 2880, partial: 14, withoutData: 0 },
    gaps: [],
    gapsTotal: 0,
    listedGapsTotal: 0,
    archive: {
      enabled: true,
      location: "/var/lib/natslens/archive",
      retention: 90 * DAY,
      oldestHour: Date.parse("2026-08-07T22:00:00Z"),
      newestHour: Date.parse("2026-11-05T22:00:00Z"),
      hours: 2161,
      lastExportAt: Date.parse("2026-11-05T22:44:18Z"),
      newestFinal: false,
      lastError: "",
      listedAt: Date.parse("2026-11-05T22:05:00Z"),
    },
    uses: [use({ id: "growth", has: DAY }), use({ id: "accept_review", needs: 8 * DAY + HOUR, has: 8 * DAY + HOUR })],
    findingsAtStart: 0,
  };
}
