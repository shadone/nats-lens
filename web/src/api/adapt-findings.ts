// Proto to view model: findings, transitions, mutes, events and the timeline.
import type { Acknowledgement as PAck } from "../gen/natslens/v1/acknowledgements_pb";
import type { Acknowledgement } from "./model";
import type { Event as PEvent } from "../gen/natslens/v1/events_pb";
import {
  FindingState as PState,
  type Finding as PFinding,
  type FindingTally,
  type FindingTransition,
  type Mute as PMute,
} from "../gen/natslens/v1/findings_pb";
import type { GetStatusTimelineResponse, LaneEvent as PLaneEvent } from "../gen/natslens/v1/timeline_pb";
import { dur, entity, id, level, ms, optSeverity, severity, source, window, frozenRange } from "./adapt-common";
import type { Finding, FindingState, LaneEvent, LensEvent, Mute, Tally, Timeline, Transition } from "./model";

const STATE: Record<PState, FindingState> = {
  [PState.UNSPECIFIED]: "unknown",
  [PState.FIRING]: "firing",
  [PState.CHANGED]: "changed",
  [PState.FLAPPING]: "flapping",
  [PState.RESOLVED]: "resolved",
};

export function findingStateEnum(s: FindingState): PState {
  const found = (Object.entries(STATE) as [string, FindingState][]).find(([, v]) => v === s);
  return found ? (Number(found[0]) as PState) : PState.UNSPECIFIED;
}

export function mute(m: PMute | undefined): Mute | undefined {
  if (!m) return undefined;
  return {
    id: m.id,
    checkCode: m.checkCode,
    entity: entity(m.entity),
    reason: m.reason,
    mutedBy: m.mutedBy,
    createdAt: ms(m.createdAt),
    expiresAt: ms(m.expiresAt),
    unmutedAt: ms(m.unmutedAt),
    unmutedBy: m.unmutedBy,
    accepted: m.accepted,
    growthPerDay: m.growthPerDay,
    endReason: m.endReason,
  };
}

export function acknowledgement(a: PAck | undefined): Acknowledgement | undefined {
  return a ? { id: a.id, findingId: a.findingId, note: a.note, ackedBy: a.ackedBy, createdAt: ms(a.createdAt), quietUntil: ms(a.quietUntil), quiet: a.quiet, endedAt: ms(a.endedAt), endedBy: a.endedBy, endReason: a.endReason } : undefined;
}

export function finding(f: PFinding): Finding {
  return {
    id: f.id,
    checkCode: f.checkCode,
    checkName: f.checkName,
    severity: severity(f.severity),
    state: STATE[f.state] ?? "unknown",
    entity: entity(f.entity),
    since: ms(f.since),
    conditionSince: ms(f.conditionSince),
    transitionedAt: ms(f.transitionedAt),
    resolvedAt: ms(f.resolvedAt),
    detail: f.detail,
    value: f.value
      ? { name: f.value.name, display: f.value.display, number: f.value.number, basis: f.value.basis }
      : undefined,
    remediation: f.remediation,
    relatedConfig: f.relatedConfig.map((c) => ({ key: c.key, value: c.value, highlight: c.highlight })),
    relatedConfigSource: f.relatedConfigSource,
    cause: f.cause
      ? { id: id(f.cause.id), time: ms(f.cause.time), kind: f.cause.kind, summary: f.cause.summary }
      : undefined,
    mute: mute(f.mute),
    acknowledgement: acknowledgement(f.acknowledgement),
    transitionsLastHour: f.transitionsLastHour,
    ...(f.expiry ? { expiry: true } : {}),
  };
}

export function optFinding(f: PFinding | undefined): Finding | undefined {
  return f ? finding(f) : undefined;
}

export function transition(t: FindingTransition): Transition {
  return {epoch:t.epoch>0n?t.epoch.toString():undefined,findingId:t.findingId||undefined, time: ms(t.time), state: STATE[t.state] ?? "unknown", severity: severity(t.severity), detail: t.detail };
}

export function tally(t: FindingTally | undefined): Tally {
  return {
    critical: t?.critical ?? 0,
    warning: t?.warning ?? 0,
    info: t?.info ?? 0,
    muted: t?.muted ?? 0,
    flapping: t?.flapping ?? 0,
    resolved24h: t?.resolved24h ?? 0,
    acknowledged: t?.acknowledged ?? 0,
  };
}

export function event(e: PEvent): LensEvent {
  return {
    id: id(e.id),
    time: ms(e.time),
    receivedAt: ms(e.receivedAt),
    source: source(e.source),
    kind: e.kind,
    type: e.type,
    severity: optSeverity(e.severity),
    summary: e.summary,
    server: e.server,
    account: e.account,
    accountName: e.accountName || undefined,
    stream: e.stream,
    consumer: e.consumer,
    clientName: e.clientName,
    clientHost: e.clientHost,
    reason: e.reason,
    transition: e.transition ? transition(e.transition) : undefined,
    finding: optFinding(e.finding),
    raised: e.raised.map(finding),
    raw: e.raw,
  };
}

export function laneEvent(e: PLaneEvent): LaneEvent {
  return {
    id: e.id,
    ...(e.entity?{entity:entity(e.entity)}:{}),...(e.findingId?{findingId:e.findingId}:{}),...(e.transitionEpoch>0n?{transitionEpoch:e.transitionEpoch.toString()}:{}),
    time: ms(e.time) ?? 0,
    source: source(e.source),
    kind: e.kind,
    shortLabel: e.shortLabel,
    label: e.label,
    severity: optSeverity(e.severity),
    eventId: e.eventId || undefined,
  };
}

export function timeline(r: GetStatusTimelineResponse): Timeline {
  return {
    start: ms(r.start) ?? 0,
    bucketMs: dur(r.bucket) ?? 0,
    bucketCount: r.bucketCount,
    rows: r.rows.map((row) => ({
      key: row.key || row.checkCode,
      checkCode: row.checkCode,
      checkName: row.checkName,
      severity: severity(row.severity),
      entity: entity(row.entity),
      cells: row.cells.map(level),
      now: level(row.now),
    })),
    kubernetes: r.kubernetes.map(laneEvent),
    nats: r.nats.map(laneEvent),
    windows: r.windows.map(window),
    natsTruncated: r.natsTruncated,
    findingTruncated: r.findingTruncated,
    findingReadEpoch:r.findingReadEpoch.toString(),
    eventReadId:r.eventReadId?.toString(),evidenceRange:frozenRange(r.range),
    kubernetesTruncated: r.kubernetesTruncated,
  };
}
