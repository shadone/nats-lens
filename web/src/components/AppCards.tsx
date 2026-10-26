// The Applications page's three summary cards: how the connections spread
// over the servers, the client libraries in use, and the 24-hour counts.
import type { Applications } from "../api/applications";
import { lastDayFigures, librariesText, spreadText } from "../lib/appsView";
import { fmtInt } from "../lib/format";

export function AppCards({ d }: { d: Applications }) {
  const figures = lastDayFigures(d.lastDay);
  return (
    <div className="ap-cards">
      <section className={`card ap-card${d.spread.concentrated ? " ap-card-info" : ""}`} aria-labelledby="ap-spread">
        <h2 className="ap-label" id="ap-spread">
          Spread
        </h2>
        <p className="ap-card-text">{spreadText(d.spread, d.partial)}</p>
      </section>
      <section className="card ap-card" aria-labelledby="ap-libs">
        <h2 className="ap-label" id="ap-libs">
          Client libraries
        </h2>
        <p className="ap-card-text">{librariesText(d.libraries)}</p>
      </section>
      <section className="card ap-card" aria-labelledby="ap-day">
        <h2 className="ap-label" id="ap-day">
          Last 24 h
        </h2>
        {d.eventsCaptured ? (
          <p className="ap-card-text">
            {figures.map((f, i) => (
              <span key={f.label}>
                <span className="ap-fig">
                  {d.captureGaps && f.value !== undefined && "at least "}
                  <span className="ap-v">{f.value === undefined ? "–" : fmtInt(f.value)}</span> {f.label}
                  {i < figures.length - 1 && <span aria-hidden="true"> ·</span>}
                  {i < figures.length - 1 && <span className="sr-only">,</span>}
                </span>{" "}
              </span>
            ))}
          </p>
        ) : (
          <p className="ap-card-text ap-quiet">Not known: no event captured up to this collection.</p>
        )}
      </section>
    </div>
  );
}
