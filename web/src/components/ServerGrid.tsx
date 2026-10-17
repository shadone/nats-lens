// "Every server": one square per server, grouped by cluster, coloured by a
// chosen metric. The meta leader is outlined and a server that did not
// answer is dashed. Hover or focus names the server and its value; a click
// opens it.
import { useState } from "react";
import type { ServerRow } from "../api/servers";
import { COLOUR_BY, colourScale, type ColourBy } from "../lib/serverColour";
import { clustersByName } from "../lib/servers";
import { serverPath } from "../lib/view";
import { RLink } from "./common";

function cellText(s: ServerRow, metric: string, value: string): string {
  if (!s.present) return `${s.name}: did not answer`;
  return `${s.name}: ${metric} ${value}${s.role === "meta leader" ? ", meta leader" : ""}`;
}

export function ServerGrid({ servers }: { servers: ServerRow[] }) {
  const [by, setBy] = useState<ColourBy>("cpu");
  const [hover, setHover] = useState<string>();
  const scale = colourScale(by, servers);
  const label = COLOUR_BY.find((c) => c.id === by)!.label;
  const groups = clustersByName(servers);
  const anyDown = servers.some((s) => !s.present);
  const anyNone = servers.some((s) => s.present && !scale.binOf(s));
  const anyLeader = servers.some((s) => s.role === "meta leader");
  const used = new Set(servers.filter((s) => s.present).map((s) => scale.binOf(s)?.cls));
  // A fixed scale shows all its bins; the version scale shows the versions in use.
  const bins = scale.bins.filter((b) => by !== "version" || used.has(b.cls));
  return (
    <section className="card sg" aria-labelledby="sg-title">
      <div className="sg-h">
        <h2 id="sg-title" className="srv-h2">
          Every server
        </h2>
        <div className="chips" role="group" aria-label="Colour by">
          <span className="lbl">Colour by</span>
          {COLOUR_BY.map((c) => (
            <button key={c.id} type="button" className={`chip${c.id === by ? " on" : ""}`} aria-pressed={c.id === by} onClick={() => setBy(c.id)}>
              {c.label}
            </button>
          ))}
        </div>
      </div>
      <ul className="sg-legend" aria-label={`Colours for ${label}`}>
        {bins.map((b) => (
          <li key={b.cls + b.label}>
            <span className={`cell-sw ${b.cls}`} aria-hidden="true" />
            {by === "version" ? <span className="mono">{b.label}</span> : b.label}
          </li>
        ))}
        {anyNone && (
          <li>
            <span className="cell-sw none" aria-hidden="true" />
            not reported
          </li>
        )}
        {anyDown && (
          <li>
            <span className="cell-sw down" aria-hidden="true" />
            did not answer
          </li>
        )}
        {anyLeader && (
          <li>
            <span className="cell-sw heat-1 leader" aria-hidden="true" />
            meta leader
          </li>
        )}
      </ul>
      <div className="sg-groups">
        {groups.map((g) => (
          <div key={g.cluster} className="sg-group">
            {groups.length > 1 && (
              <div className="sg-name">
                {g.cluster || "no cluster"} <span>· {g.rows.length}</span>
              </div>
            )}
            <div className="sg-cells" style={{ gridTemplateColumns: `repeat(${Math.min(10, Math.max(3, Math.ceil(Math.sqrt(g.rows.length * 2))))}, var(--cell))` }}>
              {g.rows.map((s) => {
                const bin = s.present ? scale.binOf(s) : undefined;
                const text = cellText(s, label, scale.valueText(s));
                const cls = ["cell", s.present ? (bin?.cls ?? "none") : "down", s.role === "meta leader" ? "leader" : ""].filter(Boolean).join(" ");
                return (
                  <RLink
                    key={s.name}
                    to={serverPath(s.name)}
                    className={cls}
                    title={text}
                    aria-label={text}
                    onMouseEnter={() => setHover(text)}
                    onMouseLeave={() => setHover(undefined)}
                    onFocus={() => setHover(text)}
                    onBlur={() => setHover(undefined)}
                  />
                );
              })}
            </div>
          </div>
        ))}
      </div>
      <p className="sg-read" aria-live="polite">
        {hover ?? "Hover a square for its name and value; click to open the server."}
      </p>
    </section>
  );
}
