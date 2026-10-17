// Proto to view model: facts about this nats-lens instance.
import { IdentityMode as PMode, type GetLensInfoResponse } from "../gen/natslens/v1/info_pb";
import { dur, ms } from "./adapt-common";
import type { IdentityMode, LensInfo } from "./model";

const MODE: Record<PMode, IdentityMode> = {
  [PMode.UNSPECIFIED]: "unknown",
  [PMode.IAP]: "iap",
  [PMode.REQUEST]: "request",
};

export function lensInfo(r: GetLensInfoResponse): LensInfo {
  return {
    identityMode: MODE[r.identityMode] ?? "unknown",
    version: r.version,
    cluster: r.cluster,
    collectIntervalMs: dur(r.collectInterval) ?? 30_000,
    retentionMs: dur(r.retention) ?? 0,
    live: r.live,
    demo: r.demo,
    historyStart: ms(r.historyStart),
  };
}
