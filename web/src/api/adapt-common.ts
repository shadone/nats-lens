// Proto to view model: enums, references, series and other shared messages.
import {toJson} from "@bufbuild/protobuf";
import {TimestampSchema} from "@bufbuild/protobuf/wkt";
import type { Duration, Timestamp } from "@bufbuild/protobuf/wkt";
import { durationMs, timestampMs } from "@bufbuild/protobuf/wkt";
import {
  EntityType,
  Severity,
  Status,
  type ConfigValue,
  type Disagreement as PDisagreement,
  type EntityRef,
  type TimeRange,
} from "../gen/natslens/v1/common_pb";
import { EventSource } from "../gen/natslens/v1/events_pb";
import type { Freshness as PFreshness, TimeWindow } from "../gen/natslens/v1/overview_pb";
import { Unit as PUnit, type Series as PSeries } from "../gen/natslens/v1/timeseries_pb";
import type {
  ConfigItem,
  Disagreement,
  Entity,
  EntityKind,
  EventSourceName,
  Freshness,
  Level,
  Range,
  Series,
  SeverityLevel,
  Unit,
  Window,
  FrozenEvidenceRange,
} from "./model";

export function ms(t: Timestamp | undefined): number | undefined {
  return t ? timestampMs(t) : undefined;
}

export function dur(d: Duration | undefined): number | undefined {
  return d ? durationMs(d) : undefined;
}

/** int64 fields arrive as bigint; optional ones may be unset. */
export function n(v: bigint | number | undefined): number {
  return v === undefined ? 0 : Number(v);
}

export function optN(v: bigint | number | undefined | null): number | undefined {
  return v === undefined || v === null ? undefined : Number(v);
}

export function id(v: bigint | number | string): string {
  return String(v);
}

export function level(s: Status): Level {
  switch (s) {
    case Status.CRITICAL:
      return "critical";
    case Status.WARNING:
      return "warning";
    case Status.INFO:
      return "info";
    case Status.MUTED:
      return "muted";
    case Status.OK:
      return "ok";
    default:
      return "nodata";
  }
}

export function severity(s: Severity): SeverityLevel {
  if (s === Severity.CRITICAL) return "critical";
  if (s === Severity.WARNING) return "warning";
  return "info";
}

/** Severity for colouring; unset for routine events. */
export function optSeverity(s: Severity): SeverityLevel | undefined {
  return s === Severity.UNSPECIFIED ? undefined : severity(s);
}

export function severityEnum(s: SeverityLevel): Severity {
  return s === "critical" ? Severity.CRITICAL : s === "warning" ? Severity.WARNING : Severity.INFO;
}

const KIND: Record<EntityType, EntityKind> = {
  [EntityType.UNSPECIFIED]: "unknown",
  [EntityType.SYSTEM]: "system",
  [EntityType.SERVER]: "server",
  [EntityType.ACCOUNT]: "account",
  [EntityType.STREAM]: "stream",
  [EntityType.CONSUMER]: "consumer",
  [EntityType.CONNECTION]: "connection",
  [EntityType.CLIENT]: "client",
};

export function entity(e: EntityRef | undefined): Entity | undefined {
  if (!e) return undefined;
  return {
    kind: KIND[e.type] ?? "unknown",
    key: e.key,
    name: e.name,
    account: e.account,
    accountName: e.accountName || undefined,
    stream: e.stream,
    consumer: e.consumer,
    server: e.server,
  };
}

/** An entity that must be present; a missing one becomes an empty reference. */
export function entityOrEmpty(e: EntityRef | undefined): Entity {
  return entity(e) ?? { kind: "unknown", key: "", name: "", account: "", stream: "", consumer: "", server: "" };
}

export function entityKindEnum(k: EntityKind): EntityType {
  const found = (Object.entries(KIND) as [string, EntityKind][]).find(([, v]) => v === k);
  return found ? (Number(found[0]) as EntityType) : EntityType.UNSPECIFIED;
}

const SOURCE: Record<EventSource, EventSourceName> = {
  [EventSource.UNSPECIFIED]: "other",
  [EventSource.NATS]: "nats",
  [EventSource.KUBERNETES]: "kubernetes",
  [EventSource.FINDING]: "finding",
  [EventSource.COLLECTOR]: "collector",
};

export function source(s: EventSource): EventSourceName {
  return SOURCE[s] ?? "other";
}

export function sourceEnum(s: EventSourceName): EventSource {
  const found = (Object.entries(SOURCE) as [string, EventSourceName][]).find(([, v]) => v === s);
  return found ? (Number(found[0]) as EventSource) : EventSource.UNSPECIFIED;
}

const UNIT: Record<PUnit, Unit> = {
  [PUnit.UNSPECIFIED]: "unknown",
  [PUnit.COUNT]: "count",
  [PUnit.BYTES]: "bytes",
  [PUnit.PER_SECOND]: "per_second",
  [PUnit.PERCENT]: "percent",
  [PUnit.SECONDS]: "seconds",
  [PUnit.BYTES_PER_DAY]: "bytes_per_day",
  [PUnit.BYTES_PER_SECOND]: "bytes_per_second",
};

export function unit(u: PUnit): Unit {
  return UNIT[u] ?? "unknown";
}

export function series(s: PSeries | undefined): Series | undefined {
  if (!s) return undefined;
  return {
    metric: s.metric,
    entity: entity(s.entity),
    unit: unit(s.unit),
    stepMs: dur(s.step) ?? 0,
    points: s.points.map((p) => ({ time: ms(p.time) ?? 0, value: p.value, low: p.low, high: p.high })),
    min: s.min,
    max: s.max,
  };
}

export function range(r: TimeRange | undefined): Range | undefined {
  const start = ms(r?.start);
  const end = ms(r?.end);
  return start !== undefined && end !== undefined ? { start, end } : undefined;
}

export function frozenRange(r:TimeRange|undefined):FrozenEvidenceRange|undefined {
 return r?.start&&r.end?{start:toJson(TimestampSchema,r.start) as string,end:toJson(TimestampSchema,r.end) as string}:undefined;
}

export function window(w: TimeWindow): Window {
  return { start: ms(w.start) ?? 0, end: ms(w.end) ?? 0, label: w.label, detail: w.detail,
 ...(w.id?{id:w.id,supportingEventIds:w.supportingEventIds,derivation:w.derivation,unavailable:w.unavailable,evidenceTotal:w.evidenceTotal}:{}),
 ...(w.eventReadId!==undefined?{eventReadId:w.eventReadId.toString()}:{}),...(w.captureRange?{captureRange:frozenRange(w.captureRange)}:{}) };
}

export function config(c: ConfigValue[]): ConfigItem[] {
  return c.map((v) => ({ key: v.key, value: v.value, highlight: v.highlight }));
}

export function disagreements(d: PDisagreement[]): Disagreement[] {
  return d.map((x) => ({ field: x.field, values: x.values.map((v) => ({ server: v.server, value: v.value })) }));
}

export function freshness(f: PFreshness | undefined): Freshness | undefined {
  if (!f) return undefined;
  return {
    epoch: String(f.epoch),
    epochAt: ms(f.epochAt),
    ageMs: dur(f.age),
    partial: f.partial,
    live: f.live,
    intervalMs: dur(f.interval) ?? 30_000,
    collecting: f.collecting,
    notCollectingSince: ms(f.notCollectingSince),
  };
}
