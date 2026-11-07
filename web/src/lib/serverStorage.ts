import type { ServerRow } from "../api/servers";
import { fmtBytes } from "./format";

/** Only current, positive limits describe a usable configured ceiling. */
export function storageLimit(s: ServerRow): number | undefined {
  return s.present && s.role !== "no JetStream" && s.jsMaxStorageBytes !== undefined && s.jsMaxStorageBytes > 0 ? s.jsMaxStorageBytes : undefined;
}

/** Per-server file storage, including local replica copies; never filesystem use. */
export function storageReading(s: ServerRow): string {
  if (!s.present) return "did not answer";
  if (s.role === "no JetStream") return "no JetStream";
  const used = s.jsStorageBytes === undefined ? "not reported" : fmtBytes(s.jsStorageBytes);
  const limit = storageLimit(s);
  return limit === undefined ? `${used} · limit unknown` : s.jsStorageBytes === undefined ? `${used} · limit ${fmtBytes(limit)}` : `${used} / ${fmtBytes(limit)}`;
}
