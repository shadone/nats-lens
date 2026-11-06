// Data history: GetDataHistoryResponse to the view model of the Data
// history page. The only file that reads its messages; a value the server
// left unset stays undefined here, never 0.
import { HistoryGapKind, type GetDataHistoryResponse, type HistoryGap as PGap } from "../gen/natslens/v1/history_pb";
import { dur, ms } from "./adapt-common";

export type GapKind = "notRunning" | "notConnected" | "noCollections" | "eventsNotCaptured" | "kubernetesNotWatched" | "beforeHistory" | "unknown";

/** The kinds charts hatch: times without collections, and the range before the history. */
export const HATCHED_KINDS: readonly GapKind[] = ["notRunning", "notConnected", "noCollections", "beforeHistory"];

export interface HistoryGap {
  kind: GapKind;
  /** May be before the range. */
  start?: number;
  /** Undefined while the gap is open at the range's end. */
  end?: number;
  /** The collection before the gap; undefined when not known. */
  beforeEpoch?: string;
  beforeEpochAt?: number;
  /** The collection after the gap; undefined when not known or none followed. */
  afterEpoch?: string;
  afterEpochAt?: number;
  /** The collector event behind the gap; empty for no collections and before history. */
  eventId: string;
  /** The connection lost, for events not captured. */
  connection: string;
}

export interface HistoryCoverage {
  /** The later of the range start and the history start; undefined without history in the range. */
  from?: number;
  to?: number;
  epochs: number;
  expected: number;
  partial: number;
  /** Time without collections in the covered span; undefined without history in the range. */
  withoutData?: number;
}

export interface ArchiveState {
  /** Whether an archiver writes the archive in this process. */
  enabled: boolean;
  location: string;
  /** Undefined when every hour is kept. */
  retention?: number;
  oldestHour?: number;
  newestHour?: number;
  /** Undefined before the archiver's first listing. */
  hours?: number;
  lastExportAt?: number;
  newestFinal: boolean;
  /** Empty without an error since the last export that succeeded. */
  lastError: string;
  lastErrorAt?: number;
  listedAt?: number;
}

export interface HistoryUse {
  /** growth, forecast:<check>, lookback:<check>, accept_review, day or replay. */
  id: string;
  checkCode: string;
  needs?: number;
  /** Undefined without history. */
  has?: number;
  enough: boolean;
  enoughAt?: number;
  /** The retention is shorter than the need. */
  never: boolean;
}

export interface ServerHistoryGap { start: number; end: number; reason: string }

export interface DataHistory {
  serverGaps?: ServerHistoryGap[];
  /** The range the request asked for. */
  rangeStart: number;
  rangeEnd: number;
  historyStart?: number;
  newestEpochAt?: number;
  eventsStart?: number;
  /** Undefined when collections and events are kept forever. */
  retention?: number;
  retentionFrom?: number;
  intervalMs?: number;
  coverage: HistoryCoverage;
  /** Newest first. */
  gaps: HistoryGap[];
  /** All hatched gaps of the range, of which gaps holds at most 200. */
  gapsTotal: number;
  /** All other gaps of the range, of which gaps holds at most 200. */
  listedGapsTotal: number;
  archive: ArchiveState;
  uses: HistoryUse[];
  findingsAtStart: number;
}

const KIND: Record<HistoryGapKind, GapKind> = {
  [HistoryGapKind.UNSPECIFIED]: "unknown",
  [HistoryGapKind.NOT_RUNNING]: "notRunning",
  [HistoryGapKind.NOT_CONNECTED]: "notConnected",
  [HistoryGapKind.NO_COLLECTIONS]: "noCollections",
  [HistoryGapKind.EVENTS_NOT_CAPTURED]: "eventsNotCaptured",
  [HistoryGapKind.KUBERNETES_NOT_WATCHED]: "kubernetesNotWatched",
  [HistoryGapKind.BEFORE_HISTORY]: "beforeHistory",
};

/** An epoch number, or undefined for 0, which the API sends for none. */
function epoch(v: bigint): string | undefined {
  return v === 0n ? undefined : String(v);
}

function gap(g: PGap): HistoryGap {
  return {
    kind: KIND[g.kind] ?? "unknown",
    start: ms(g.start),
    end: ms(g.end),
    beforeEpoch: epoch(g.beforeEpoch),
    beforeEpochAt: ms(g.beforeEpochAt),
    afterEpoch: epoch(g.afterEpoch),
    afterEpochAt: ms(g.afterEpochAt),
    eventId: g.eventId,
    connection: g.connection,
  };
}

export function isHatched(g: HistoryGap): boolean {
  return HATCHED_KINDS.includes(g.kind);
}

export function dataHistory(r: GetDataHistoryResponse, range: { start: number; end: number }): DataHistory {
  const c = r.coverage;
  const a = r.archive;
  const listedAt = ms(a?.listedAt);
  return {
    rangeStart: range.start,
    rangeEnd: range.end,
    historyStart: ms(r.historyStart),
    newestEpochAt: ms(r.newestEpochAt),
    eventsStart: ms(r.eventsStart),
    retention: dur(r.retention),
    retentionFrom: ms(r.retentionFrom),
    intervalMs: dur(r.collectInterval),
    coverage: {
      from: ms(c?.coveredFrom),
      to: ms(c?.coveredTo),
      epochs: Number(c?.epochs ?? 0n),
      expected: Number(c?.expectedEpochs ?? 0n),
      partial: Number(c?.partialEpochs ?? 0n),
      withoutData: dur(c?.withoutData),
    },
    gaps: r.gaps.map(gap),
    serverGaps: r.serverGaps.map(g => ({ start: ms(g.start)!, end: ms(g.end)!, reason: g.reason })),
    gapsTotal: r.gapsTotal,
    listedGapsTotal: r.listedGapsTotal,
    archive: {
      enabled: a?.enabled ?? false,
      location: a?.location ?? "",
      retention: dur(a?.archiveRetention),
      oldestHour: ms(a?.oldestHour),
      newestHour: ms(a?.newestHour),
      // Hours are counted at a listing; before the first there is no count.
      hours: listedAt === undefined ? undefined : (a?.hours ?? 0),
      lastExportAt: ms(a?.lastExportAt),
      newestFinal: a?.newestFinal ?? false,
      lastError: a?.lastError ?? "",
      lastErrorAt: ms(a?.lastErrorAt),
      listedAt,
    },
    uses: r.uses.map((u) => ({
      id: u.id,
      checkCode: u.checkCode,
      needs: dur(u.needs),
      has: dur(u.has),
      enough: u.enough,
      enoughAt: ms(u.enoughAt),
      never: u.never,
    })),
    findingsAtStart: r.findingsAtStart,
  };
}
