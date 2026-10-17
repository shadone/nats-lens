// Placeholder for the Fleet page: the systems by name, each a link to its
// Overview. The Fleet page with a card per system replaces it.
import { useSystems } from "../api/systems";
import { Empty, QueryView, RLink } from "../components/common";
import { usePageTitle } from "../lib/usePageTitle";

export function Fleet() {
  usePageTitle("Fleet");
  const q = useSystems();
  return (
    <>
      <div className="page-h">
        <h1>Fleet</h1>
      </div>
      <QueryView q={q} what="systems">
        {(d) =>
          d.systems.length === 0 ? (
            <Empty>No systems.</Empty>
          ) : (
            <ul className="card fleet-list">
              {d.systems.map((s) => (
                <li key={s.id}>
                  <RLink to={`/?system=${encodeURIComponent(s.id)}`}>{s.name}</RLink>
                  {s.environment && <span className="aside">{s.environment}</span>}
                </li>
              ))}
            </ul>
          )
        }
      </QueryView>
    </>
  );
}
