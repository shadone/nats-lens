// Proto to view model: the shell's status and each system's badge.
import type { GetShellStatusResponse, SystemStatus } from "../gen/natslens/v1/systems_pb";
import { freshness, level } from "./adapt-common";
import { server } from "./adapt-entities";
import { tally } from "./adapt-findings";
import { collection } from "./adapt-systems";
import type { ShellStatus, SystemBadge } from "./model";

export function systemBadge(s: SystemStatus): SystemBadge {
  const id = s.system?.id ?? "";
  return {
    id,
    name: s.system?.name || id,
    environment: s.system?.environment ?? "",
    status: level(s.status),
    collection: collection(s.collection),
    collecting: s.collecting,
    error: s.error,
    pending: s.pending,
  };
}

export function shellStatus(r: GetShellStatusResponse): ShellStatus {
  return {
    freshness: freshness(r.freshness),
    servers: r.servers.map(server),
    tally: tally(r.tally),
    systems: r.systems.map(systemBadge),
    defaultSystem: r.defaultSystem || r.systems[0]?.system?.id || "",
    systemKnown: r.systemKnown,
    error: r.errorCode ? { code: r.errorCode, message: r.error } : undefined,
  };
}
