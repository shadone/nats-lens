// Pieces shared by the list and detail pages: pager, stat cards, consumer
// table, disagreements, findings and events for one entity.
import { useState, type ReactNode } from "react";
import type { Disagreement, Entity } from "../api/model";
import { useEvents, useFindings } from "../api/queries";
import { useResolvedRange, useTimeRange } from "../lib/useTimeRange";
import { Dash, Empty, QueryView, RLink, Section } from "./common";
import { EventRow } from "./EventRow";
import { FindingRow } from "./FindingParts";

export function Stat({ label, value, sub, bad }: { label: string; value: ReactNode; sub?: ReactNode; bad?: boolean }) {
  return (
    <div className="card stat">
      <span className="lbl">{label}</span>
      <span className={`v${bad ? " bad" : ""}`}>{value}</span>
      {sub && <small>{sub}</small>}
    </div>
  );
}

export function Disagreements({ items }: { items: Disagreement[] }) {
  if (!items.length) return null;
  const servers = [...new Set(items.flatMap((d) => d.values.map((v) => v.server)))];
  return (
    <Section title="Servers disagree" aside="Each server's reported configuration and metadata. A differing creation timestamp alone does not establish its cause or replica health.">
      <div className="card table-wrap disagree">
        <table className="tbl">
          <thead>
            <tr>
              <th scope="col">Setting</th>
              {servers.map((s) => (
                <th scope="col" key={s}>
                  {s}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {items.map((d) => (
              <tr key={d.field}>
                <td className="mono">{d.field === "created" ? "Reported creation time" : d.field}</td>
                {servers.map((s) => (
                  <td key={s} className="mono">
                    {d.values.find((v) => v.server === s)?.value ?? <Dash />}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Section>
  );
}

/** Current findings for an entity. */
export function EntityFindings({ entity }: { entity: Entity }) {
  const { spec } = useTimeRange();
  const { start, end } = useResolvedRange(spec);
  const q = useFindings(spec, { entity, includeMuted: true });
  return (
    <Section title="Findings" aside="Firing now for this entity, worst first.">
      <QueryView q={q} what="findings">
        {(p) =>
          p.items.length ? (
            <div className="card others">
              {p.items.map((f) => (
                <FindingRow key={f.id} f={f} end={end} rangeStart={start} />
              ))}
            </div>
          ) : (
            <Empty>No finding fires for this entity.</Empty>
          )
        }
      </QueryView>
    </Section>
  );
}

/** The newest events that concern an entity, with a link to all of them. */
export function EntityEvents({ entity, query }: { entity: Entity; query: string }) {
  const { spec } = useTimeRange();
  const { end } = useResolvedRange(spec);
  const q = useEvents(spec, { entity, pageSize: 15 });
  const [open, setOpen] = useState<string>();
  return (
    <Section title="Events" aside={<RLink to={`/events?${query}`}>All events for this entity</RLink>}>
      <QueryView q={q} what="events">
        {(p) =>
          p.items.length ? (
            <div className="card evlist">
              {p.items.map((e) => (
                <EventRow key={e.id} e={e} end={end} open={open === e.id} onToggle={() => setOpen(open === e.id ? undefined : e.id)} />
              ))}
            </div>
          ) : (
            <Empty>No events for this entity in the range.</Empty>
          )
        }
      </QueryView>
    </Section>
  );
}
