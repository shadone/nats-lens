import { deferredView } from "../components/deferredView";
// Micro services: the services built on the NATS micro framework in the
// range's newest collection, a card each, and the chosen card's endpoints
// and instances. The chosen service is kept in the address as
// ?service=<account>/<name>.
import { useMicroServices } from "../api/queries";
import { useAccounts } from "../api/systems";
import { usePageTitle } from "../lib/usePageTitle";
import { useAccountScope } from "../lib/useSystem";
import { useTimeRange } from "../lib/useTimeRange";

export const SERVICE_PARAM = "service";

function useMicroServicesModel() {
  usePageTitle("Micro services");
  const { spec } = useTimeRange();
  const [account] = useAccountScope();
  const q = useMicroServices(spec, account);
  // The header's account list names an account the response leaves unnamed.
  const listed = useAccounts(spec).data;
  const names = new Map((listed ?? []).filter((a) => a.name).map((a) => [a.account, a.name]));
  return { account, q: { ...q }, names };
}
export type MicroServicesModel = ReturnType<typeof useMicroServicesModel>;
const View = deferredView(() => import("../views/MicroServices").then(m => m.MicroServicesView));
export function MicroServices() {
  const model = useMicroServicesModel();
  return <View model={model} />;
}
