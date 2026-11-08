import { deferredView } from "../components/deferredView";
// Applications: the client connections of the range's newest collection
// grouped by the application in their name, with three summary cards,
// a table that sorts by any column (kept in the address as ?sort=) and a
// footnote. ?tooling=1 lists the tooling connections too, as on the
// Connections page.
import { useSearchParams } from "react-router";
import { useApplications, useLensInfo } from "../api/queries";
import { useAccounts } from "../api/systems";
import { TOOLING_PARAM } from "../lib/appsView";
import { replayEntry } from "../lib/replayEntry";
import { usePageTitle } from "../lib/usePageTitle";
import { useAccountScope } from "../lib/useSystem";
import { useNow, useResolvedRange, useTimeRange } from "../lib/useTimeRange";

function useApplicationsModel() {
  usePageTitle("Applications");
  const [params, setParams] = useSearchParams();
  const { spec } = useTimeRange();
  const [account] = useAccountScope();
  const range = useResolvedRange(spec);
  const now = useNow(30_000);
  const minGapMs = useLensInfo().data?.collectIntervalMs ?? 30_000;
  const context = replayEntry({ a: Math.max(range.start, range.end - 86_400_000), b: range.end }, now, minGapMs);
  const tooling = params.get(TOOLING_PARAM) === "1";
  const q = useApplications(spec, account, tooling);
  // The header's account list names an account the response leaves unnamed.
  const listed = useAccounts(spec).data;
  const names = new Map((listed ?? []).filter((a) => a.name).map((a) => [a.account, a.name]));
  const setTooling = (on: boolean) =>
    setParams(
      (p) => {
        const n = new URLSearchParams(p);
        if (on) n.set(TOOLING_PARAM, "1");
        else n.delete(TOOLING_PARAM);
        return n;
      },
      { replace: true },
    );
  return { account, context, tooling, q: { ...q }, names, setTooling };
}
export type ApplicationsModel = ReturnType<typeof useApplicationsModel>;
const View = deferredView(() => import("../views/Applications").then(m => m.ApplicationsView));
export function Applications() {
  const model = useApplicationsModel();
  return <View model={model} />;
}
