// Proto to view model: the summary of each system for the Fleet page.
import { CollectionState, type SystemSummary as PSummary } from "../gen/natslens/v1/systems_pb";
import { dur, freshness, level, ms, optN } from "./adapt-common";
import { optFinding, tally } from "./adapt-findings";
import type { Collection, SystemSummary } from "./model";

const COLLECTION: Record<CollectionState, Collection> = {
  [CollectionState.UNSPECIFIED]: "unknown",
  [CollectionState.STARTING]: "starting",
  [CollectionState.COLLECTING]: "collecting",
  [CollectionState.NOT_CONNECTED]: "not connected",
  [CollectionState.CONNECTION_CLOSED]: "connection closed",
  [CollectionState.DEMO]: "demo",
};

export function collection(c: CollectionState): Collection {
  return COLLECTION[c] ?? "unknown";
}

export function systemSummary(s: PSummary): SystemSummary {
  const id = s.system?.id ?? "";
  return {
    id,
    name: s.system?.name || id,
    environment: s.system?.environment ?? "",
    collection: collection(s.collection),
    collectionError: s.collectionError,
    collectionErrorSince: ms(s.collectionErrorSince),
    freshness: freshness(s.freshness),
    status: level(s.status),
    worst: optFinding(s.worstFinding),
    tally: tally(s.tally),
    servers: optN(s.servers),
    serversAnswering: optN(s.serversAnswering),
    clusters: optN(s.clusters),
    streams: optN(s.streams),
    connections: optN(s.connections),
    strip: s.strip.map(level),
    stripStart: ms(s.stripStart),
    stripBucketMs: dur(s.stripBucket),
    error: s.error,
  };
}
