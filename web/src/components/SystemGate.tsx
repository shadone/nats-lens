// What stands between the URL and a page: an unknown ?system= shows
// "System not found" instead of a page whose every call would fail, and
// "/" lands on Fleet when there are several systems and none is named.
import type { ReactNode } from "react";
import { Navigate, useLocation } from "react-router";
import { useSystems } from "../api/systems";
import { globalQuery } from "../lib/globalQuery";
import { usePageTitle } from "../lib/usePageTitle";
import { useSystem } from "../lib/useSystem";
import { useTimeRange } from "../lib/useTimeRange";
import { navFor } from "../nav";
import { Loading, RLink } from "./common";

function SystemNotFound({ id }: { id: string }) {
  usePageTitle("System not found");
  return (
    <div className="page-h">
      <h1>System not found</h1>
      <span className="aside">
        This nats-lens observes no system <code className="mono">{id}</code>. <RLink to="/fleet">Fleet</RLink> lists the systems it does.
      </span>
    </div>
  );
}

export function SystemGate({ children }: { children: ReactNode }) {
  const param = useSystem();
  const { pathname } = useLocation();
  const q = useSystems();
  if (!param || navFor(pathname)?.crossSystem) return children;
  // Without the list of systems the page is shown; its calls say what is wrong.
  if (q.isPending) return <Loading what="systems" />;
  if (q.data && !q.data.systems.some((s) => s.id === param)) return <SystemNotFound id={param} />;
  return children;
}

/** "/": Fleet when several systems exist and the link names none, else the page given. */
export function Landing({ children }: { children: ReactNode }) {
  const param = useSystem();
  const { spec } = useTimeRange();
  const q = useSystems();
  if (param) return children;
  if (q.isPending) return <Loading what="systems" />;
  if (q.data && q.data.systems.length > 1) return <Navigate replace to={`/fleet${globalQuery(spec, "", "")}`} />;
  return children;
}
