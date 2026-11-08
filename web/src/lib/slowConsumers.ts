import { globalQuery } from "./globalQuery";

/** Freeze an investigation or evidence link at the observed absolute range. */
export function slowEvidencePath(path: string, start: number, end: number, global: string, server?: string) {
  const scope = new URLSearchParams(global);
  const query = new URLSearchParams(globalQuery({ kind: "fixed", from: start, to: end }, scope.get("system") ?? "", scope.get("account") ?? ""));
  if (server) query.set("server", server);
  const [base, own] = path.split("?");
  new URLSearchParams(own).forEach((v, k) => query.set(k, v));
  return `${base}?${query}`;
}
