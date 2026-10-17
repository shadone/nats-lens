// KV buckets and object stores of one system: requests, view models and
// hooks. Every hook keys on the page's system and account scope.
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import type { KeyValueStore, ObjectStore } from "../gen/natslens/v1/buckets_pb";
import { rangeKey, type RangeSpec } from "../lib/timeRange";
import { useSystem } from "../lib/useSystem";
import { dur, entityOrEmpty, level, optN } from "./adapt-common";
import { placement, type Placement } from "./adapt-placement";
import { lens } from "./client";
import type { Entity, Level } from "./model";
import { LIVE_REFRESH_MS } from "./queries";
import { rangeInit, systemInit } from "./requests";

/** What KV buckets and object stores share. Numbers not observed are unset. */
export interface Bucket {
  /** The stream that holds the bucket: KV_<bucket> or OBJ_<bucket>. */
  entity: Entity;
  bucket: string;
  present: boolean;
  expected: boolean;
  replicas?: number;
  storage: string;
  bytes?: number;
  maxBytes?: number;
  /** Unset when not reported; 0 when nothing expires. */
  ttlMs?: number;
  bytesPerDay?: number;
  description: string;
  status: Level;
  findingCodes: string[];
  placement: Placement[];
}

export interface KvBucket extends Bucket {
  history?: number;
  /** Subjects, which count deleted keys too. */
  keys?: number;
  messages?: number;
  watchers?: number;
  compression: string;
  mirror: string;
  allowMsgTtl: boolean;
}

export interface ObjectBucket extends Bucket {
  /** Messages: the chunks and one metadata message per object. */
  chunks?: number;
  sealed: boolean;
}

function bucket(b: KeyValueStore | ObjectStore): Bucket {
  return {
    entity: entityOrEmpty(b.entity),
    bucket: b.bucket,
    present: b.present,
    expected: b.expected,
    replicas: b.replicas,
    storage: b.storage,
    bytes: optN(b.bytes),
    maxBytes: optN(b.maxBytes),
    ttlMs: dur(b.ttl),
    bytesPerDay: b.bytesPerDay,
    description: b.description,
    status: level(b.status),
    findingCodes: b.findingCodes,
    placement: placement(b.placement),
  };
}

export function kvBucket(b: KeyValueStore): KvBucket {
  return {
    ...bucket(b),
    history: b.history,
    keys: optN(b.keys),
    messages: optN(b.messages),
    watchers: b.watchers,
    compression: b.compression,
    mirror: b.mirror,
    allowMsgTtl: b.allowMsgTtl,
  };
}

export function objectBucket(b: ObjectStore): ObjectBucket {
  return { ...bucket(b), chunks: optN(b.chunks), sealed: b.sealed };
}

export async function listKeyValueStores(sys: string, spec: RangeSpec, account: string, signal?: AbortSignal): Promise<KvBucket[]> {
  const r = await lens.listKeyValueStores({ ...systemInit(sys), range: rangeInit(spec), account }, { signal });
  return r.keyValueStores.map(kvBucket);
}

export async function listObjectStores(sys: string, spec: RangeSpec, account: string, signal?: AbortSignal): Promise<ObjectBucket[]> {
  const r = await lens.listObjectStores({ ...systemInit(sys), range: rangeInit(spec), account }, { signal });
  return r.objectStores.map(objectBucket);
}

function refetch(spec: RangeSpec) {
  return spec.kind === "live" ? LIVE_REFRESH_MS : false;
}

/** The KV buckets of the page's system in the account ("" for all). */
export function useKeyValueStores(spec: RangeSpec, account: string) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["kvStores", sys, rangeKey(spec), account],
    queryFn: ({ signal }) => listKeyValueStores(sys, spec, account, signal),
    refetchInterval: refetch(spec),
    placeholderData: keepPreviousData,
  });
}

/** The object stores of the page's system in the account ("" for all). */
export function useObjectStores(spec: RangeSpec, account: string) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["objectStores", sys, rangeKey(spec), account],
    queryFn: ({ signal }) => listObjectStores(sys, spec, account, signal),
    refetchInterval: refetch(spec),
    placeholderData: keepPreviousData,
  });
}
