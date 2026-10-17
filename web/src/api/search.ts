// The search in the header: a jump to a server, stream, consumer, KV store,
// object store or account of the current system, by name. It searches the
// list calls the pages already make; there is no search call in the API.
import { useQuery } from "@tanstack/react-query";
import { rangeKey, type RangeSpec } from "../lib/timeRange";
import { useSystem } from "../lib/useSystem";
import { consumerPath, serverPath, streamPath } from "../lib/view";
import { entityOrEmpty } from "./adapt-common";
import { lens } from "./client";
import { listConsumers, listServers, listStreams, rangeInit, systemInit } from "./requests";
import { listAccounts, useCurrentSystem } from "./systems";

export type SearchKind = "server" | "stream" | "consumer" | "kv" | "object" | "account";

export const SEARCH_KIND_LABEL: Record<SearchKind, string> = {
  server: "Server",
  stream: "Stream",
  consumer: "Consumer",
  kv: "KV store",
  object: "Object store",
  account: "Account",
};

export interface SearchItem {
  kind: SearchKind;
  name: string;
  /** What tells two items of the same name apart: an account, a stream, a cluster. */
  sub: string;
  /** Page to open; a path that starts with "?" stays on the current page with these parameters. */
  path: string;
}

/** Items whose name contains the text, names that start with it first. */
export function searchItems(items: SearchItem[], text: string, limit = 8): SearchItem[] {
  const t = text.trim().toLowerCase();
  if (!t) return [];
  return items
    .filter((i) => i.name.toLowerCase().includes(t))
    .map((i) => ({ i, starts: i.name.toLowerCase().startsWith(t) ? 0 : 1 }))
    .sort((a, b) => a.starts - b.starts || a.i.name.localeCompare(b.i.name))
    .slice(0, limit)
    .map((x) => x.i);
}

async function loadSearchItems(sys: string, spec: RangeSpec, signal?: AbortSignal): Promise<SearchItem[]> {
  const range = rangeInit(spec);
  const parts = await Promise.allSettled([
    listServers(sys, spec, signal).then((r) => r.servers.map((s): SearchItem => ({ kind: "server", name: s.name, sub: s.cluster, path: serverPath(s.name) }))),
    listStreams(sys, spec, false, signal).then((r) =>
      r.streams.map((s): SearchItem => ({ kind: "stream", name: s.entity.stream, sub: s.entity.accountName || s.entity.account, path: streamPath(s.entity.account, s.entity.stream) })),
    ),
    listConsumers(sys, spec, {}, signal).then((cs) =>
      cs.map((c): SearchItem => ({ kind: "consumer", name: c.entity.consumer, sub: c.entity.stream, path: consumerPath(c.entity.account, c.entity.stream, c.entity.consumer) })),
    ),
    lens.listKeyValueStores({ ...systemInit(sys), range }, { signal }).then((r) =>
      r.keyValueStores.map((k): SearchItem => {
        const e = entityOrEmpty(k.entity);
        return { kind: "kv", name: k.bucket, sub: e.accountName || e.account, path: streamPath(e.account, e.stream || `KV_${k.bucket}`) };
      }),
    ),
    lens.listObjectStores({ ...systemInit(sys), range }, { signal }).then((r) =>
      r.objectStores.map((o): SearchItem => {
        const e = entityOrEmpty(o.entity);
        return { kind: "object", name: o.bucket, sub: e.accountName || e.account, path: streamPath(e.account, e.stream || `OBJ_${o.bucket}`) };
      }),
    ),
    listAccounts(sys, spec, signal).then((as) =>
      as.map((a): SearchItem => ({ kind: "account", name: a.name || a.account, sub: a.name ? a.account : "", path: `?account=${encodeURIComponent(a.account)}` })),
    ),
  ]);
  const found = parts.flatMap((p) => (p.status === "fulfilled" ? p.value : []));
  const failed = parts.find((p): p is PromiseRejectedResult => p.status === "rejected");
  if (found.length === 0 && failed) throw failed.reason;
  return found;
}

/** The searchable names of the current system; loaded only once the search is used. */
export function useSearchItems(spec: RangeSpec, enabled: boolean) {
  const sys = useSystem();
  const known = useCurrentSystem().known;
  return useQuery({
    queryKey: ["search", sys, rangeKey(spec)],
    queryFn: ({ signal }) => loadSearchItems(sys, spec, signal),
    enabled: enabled && known === true,
    staleTime: 60_000,
  });
}
