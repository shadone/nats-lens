import { deferredView } from "../components/deferredView";
// Data history: what nats-lens holds of a system: since when, how long it
// keeps it, where the range has gaps, what uses the history, and what the
// archive holds.
import { useDataHistory, useShellStatus } from "../api/queries";
import { usePageTitle } from "../lib/usePageTitle";
import { useNow, useTimeRange } from "../lib/useTimeRange";

function useDataHistoryModel() {
  usePageTitle("Data history");
  const { spec } = useTimeRange();
  const live = spec.kind === "live";
  const q = useDataHistory(spec);
  const shell = useShellStatus();
  const now = useNow(30_000);
  const f = shell.data?.freshness;
  const stopped = f && !f.collecting ? f.notCollectingSince : undefined;
  const d = q.data;
  return { live, q: { ...q }, now, stopped, d };
}
export type DataHistoryModel = ReturnType<typeof useDataHistoryModel>;
const View = deferredView(() => import("../views/DataHistory").then(m => m.DataHistoryView));
export function DataHistory() {
  const model = useDataHistoryModel();
  return <View model={model} />;
}
