import { deferredView } from "../components/deferredView";
// Accounts: the accounts of a system in the range's newest collection with
// their limits, the failed logins of the 24 hours before it, and the
// certificates and JWTs that expire soon.
import { useAccountsAndAuth } from "../api/queries";
import { usePageTitle } from "../lib/usePageTitle";
import { useTimeRange } from "../lib/useTimeRange";

function useAccountsModel() {
  usePageTitle("Accounts");
  const { spec } = useTimeRange();
  const q = useAccountsAndAuth(spec);
  const d = q.data;
  return { q: { ...q }, d };
}
export type AccountsModel = ReturnType<typeof useAccountsModel>;
const View = deferredView(() => import("../views/Accounts").then(m => m.AccountsView));
export function Accounts() {
  const model = useAccountsModel();
  return <View model={model} />;
}
