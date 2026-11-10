// Applications: GetApplicationsResponse to the view model of the
// Applications page. The only file that reads its messages; a value the
// server left unset stays undefined here, never 0.
import type { GetApplicationsResponse } from "../gen/natslens/v1/applications_pb";
import type { Duration } from "@bufbuild/protobuf/wkt";
import { ms, optN, severity } from "./adapt-common";
import type { SeverityLevel } from "./model";

export interface AppServer {
  server: string;
  connections: number;
}

export interface AppAccount {
  account: string;
  /** "" when no name is known. */
  accountName: string;
}

export interface AppLibrary {
  /** "" when not reported. */
  lang: string;
  version: string;
  connections: number;
}

export interface AppFinding {
  id: string;
  checkCode: string;
  checkName: string;
  severity: SeverityLevel;
  muted: boolean;
  entityKey: string;
}

/** One hour of an application's traffic; a count is undefined when no collection of the hour measured it. */
export interface AppHour {
  hour: number;
  inMsgs?: number;
  outMsgs?: number;
  inBytes?: number;
  outBytes?: number;
}

export interface Application {
  name: string;
  connections: number;
  instances: number;
  /** At most 10, sorted. */
  pods: string[];
  podsTotal: number;
  servers: AppServer[];
  accounts: AppAccount[];
  libraries: AppLibrary[];
  rttMs?: number;
  subscriptions?: number;
  pendingBytes?: number;
  pendingBytesMax?: number;
  /** The 24-hour counts; undefined when no event was captured up to the epoch. */
  reconnects?: number;
  slowConsumers?: number;
  stale?: number;
  shortLived?: number;
  findings: AppFinding[];
  /** The 24 whole hours before the epoch's hour, oldest first. */
  traffic: AppHour[];
  inMsgs?: number;
  outMsgs?: number;
  inBytes?: number;
  outBytes?: number;
  tooling: boolean;
  /** At most 10, sorted. */
  addresses: string[];
  addressesTotal: number;
  /** Distinct connection names, sorted, at most 3; an unnamed connection has none here. */
  connectionNames: string[];
  /** Distinct names beyond connectionNames. */
  moreNames: number;
  /** Where the connections run; undefined while client pods are not watched. */
  runsOn?: AppRunsOn;
}

/** The workload that owns some of an application's pods. */
export interface AppWorkload {
  namespace: string;
  /** Deployment, StatefulSet, DaemonSet, CronJob, Job, ReplicaSet, another controller's kind, or Pod. */
  kind: string;
  name: string;
  pods: number;
  connections: number;
}

/** The connections matched to watched pods by their client address. */
export interface AppRunsOn {
  /** Connections matched to exactly one pod. */
  matched: number;
  /** Connections from an address a host-network pod, or more than one pod, held. */
  ambiguous: number;
  /** At most 10, most connections first. */
  workloads: AppWorkload[];
  workloadsTotal: number;
  /** At most 10, sorted. */
  nodes: string[];
  nodesTotal: number;
  pods: number;
}

/** A client pod watch that was failing at the collection. */
export interface ClientPodWatchGap {
  /** For example "client pods in shop". */
  source: string;
  since?: number;
  error: string;
}

export interface AppSpread {
  /** "" without a connection. */
  server: string;
  connections: number;
  total: number;
  servers: number;
  concentrated: boolean;
}

export interface LibraryUse {
  lang: string;
  version: string;
  applications: number;
}

export interface AppsLastDay {
  slowConsumers?: number;
  stale?: number;
  reconnecting?: number;
}

export interface Applications {
  /** False when the range holds no collection; everything else is then empty. */
  collected: boolean;
  epochAt?: number;
  applications: Application[];
  applicationsTotal: number;
  connectionsTotal: number;
  toolingHidden: number;
  spread: AppSpread;
  libraries: LibraryUse[];
  lastDay: AppsLastDay;
  connzCut: boolean;
  eventsCaptured: boolean;
  /** A server or an endpoint did not answer: the connections of a server that did not answer are not counted. */
  partial: boolean;
  /** The servers that did not answer, sorted. */
  missing: string[];
  /** Event capture stopped for part of the 24 hours: the 24-hour counts are lower bounds. */
  captureGaps: boolean;
  /** Whether client pods are watched (kubernetes.client_pod_watch); each runsOn is undefined when not. */
  clientPodsWatched: boolean;
  /** The client pod watches failing at the collection. */
  clientPodsUnwatched: ClientPodWatchGap[];
  /** The client pod watch saw more pods than it keeps: the others match no connection. */
  clientPodsCapped: boolean;
}

/** A duration in milliseconds with its fraction: a round trip of 0.69 ms must not read as 1 ms. */
function fineMs(d: Duration | undefined): number | undefined {
  return d ? Number(d.seconds) * 1000 + d.nanos / 1e6 : undefined;
}

export function applications(r: GetApplicationsResponse): Applications {
  const s = r.spread;
  return {
    collected: r.epoch !== 0n,
    epochAt: ms(r.epochAt),
    applications: r.applications.map((a) => ({
      name: a.name,
      connections: a.connections,
      instances: a.instances,
      pods: a.pods,
      podsTotal: a.podsTotal,
      servers: a.servers.map((x) => ({ server: x.server, connections: x.connections })),
      accounts: a.accounts.map((x) => ({ account: x.account, accountName: x.accountName })),
      libraries: a.libraries.map((x) => ({ lang: x.lang, version: x.version, connections: x.connections })),
      rttMs: fineMs(a.rttP50),
      subscriptions: optN(a.subscriptions),
      pendingBytes: optN(a.pendingBytes),
      pendingBytesMax: optN(a.pendingBytesMax),
      reconnects: a.reconnects24h,
      slowConsumers: a.slowConsumers24h,
      stale: a.stale24h,
      shortLived: a.shortLived24h,
      findings: a.findings.map((f) => ({ id: f.id, checkCode: f.checkCode, checkName: f.checkName, severity: severity(f.severity), muted: f.muted, entityKey: f.entityKey })),
      traffic: a.traffic.map((h) => ({ hour: ms(h.hour) ?? 0, inMsgs: optN(h.inMsgs), outMsgs: optN(h.outMsgs), inBytes: optN(h.inBytes), outBytes: optN(h.outBytes) })),
      inMsgs: optN(a.inMsgs24h),
      outMsgs: optN(a.outMsgs24h),
      inBytes: optN(a.inBytes24h),
      outBytes: optN(a.outBytes24h),
      tooling: a.tooling,
      addresses: a.addresses,
      addressesTotal: a.addressesTotal,
      connectionNames: a.connectionNames,
      moreNames: a.moreNames,
      runsOn: a.runsOn
        ? {
            matched: a.runsOn.matched,
            ambiguous: a.runsOn.ambiguous,
            workloads: a.runsOn.workloads.map((w) => ({ namespace: w.namespace, kind: w.kind, name: w.name, pods: w.pods, connections: w.connections })),
            workloadsTotal: a.runsOn.workloadsTotal,
            nodes: a.runsOn.nodes,
            nodesTotal: a.runsOn.nodesTotal,
            pods: a.runsOn.pods,
          }
        : undefined,
    })),
    applicationsTotal: r.applicationsTotal,
    connectionsTotal: r.connectionsTotal,
    toolingHidden: r.toolingHidden,
    spread: { server: s?.server ?? "", connections: s?.connections ?? 0, total: s?.total ?? 0, servers: s?.servers ?? 0, concentrated: s?.concentrated ?? false },
    libraries: r.libraries.map((l) => ({ lang: l.lang, version: l.version, applications: l.applications })),
    lastDay: { slowConsumers: r.lastDay?.slowConsumers, stale: r.lastDay?.stale, reconnecting: r.lastDay?.reconnecting },
    connzCut: r.connzCut,
    eventsCaptured: r.eventsCaptured,
    partial: r.partial,
    missing: r.missing,
    captureGaps: r.captureGaps,
    clientPodsWatched: r.clientPodsWatched,
    clientPodsUnwatched: r.clientPodsUnwatched.map((g) => ({ source: g.source, since: ms(g.since), error: g.error })),
    clientPodsCapped: r.clientPodsCapped,
  };
}
