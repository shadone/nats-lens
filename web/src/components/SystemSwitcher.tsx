// The system the pages show, at the top of the sidebar. With several
// systems it opens a menu of them; a choice opens the same page of the
// other system, with the same range and no account scope.
import { useLocation, useNavigate } from "react-router";
import { useLensInfo, useShellStatus } from "../api/queries";
import { useCurrentSystem } from "../api/systems";
import { globalQuery } from "../lib/globalQuery";
import { useTimeRange } from "../lib/useTimeRange";
import { navFor } from "../nav";
import { RLink } from "./common";
import { Menu } from "./Menu";
import { Chevron } from "./ThemeMenu";

/** "3 servers", or "2 of 3 servers" while some do not answer. */
function serverLine(servers: { present: boolean }[] | undefined): { text: string; down: boolean } | undefined {
  if (!servers || servers.length === 0) return undefined;
  const up = servers.filter((s) => s.present).length;
  const noun = servers.length === 1 ? "server" : "servers";
  return up === servers.length ? { text: `${up} ${noun}`, down: false } : { text: `${up} of ${servers.length} ${noun}`, down: true };
}

export function SystemSwitcher() {
  const { id, info, systems } = useCurrentSystem();
  const demo = useLensInfo().data?.demo;
  const servers = serverLine(useShellStatus().data?.servers);
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const { spec } = useTimeRange();
  const name = info?.name || id || "nats-lens";
  const page = navFor(pathname);
  const base = page && !page.crossSystem ? page.path : "/";

  const body = (
    <>
      <span className="sw-text">
        <span className="sw-name">{name}</span>
        <span className="sw-sub">
          {info?.environment && <span>{info.environment}</span>}
          {servers && <span className={servers.down ? "down" : undefined}>{servers.text}</span>}
          {demo && <span className="demo-tag">demo</span>}
        </span>
      </span>
    </>
  );

  if (!systems || systems.systems.length <= 1) {
    return (
      <div className="switcher single" aria-label={`System: ${name}`}>
        {body}
      </div>
    );
  }
  return (
    <Menu
      label={`System: ${name}. Switch system`}
      menuLabel="Systems"
      className="switcher"
      align="left"
      choices={systems.systems.map((s) => ({
        key: s.id,
        label: s.name,
        note: s.environment || undefined,
        checked: s.id === id,
        onSelect: () => navigate(`${base}${globalQuery(spec, s.id, "")}`),
      }))}
      footer={<RLink to="/fleet">All systems</RLink>}
    >
      {body}
      <Chevron />
    </Menu>
  );
}
