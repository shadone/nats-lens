// "What happened, in order": the steps between A and B, each with its time
// or span, a dot by kind and severity, a sentence, what it consists of and
// links to its evidence; then the steps after B that close one of them.
import type { ReplayStep } from "../api/adapt-replay";
import { momentIso, type Moments } from "../lib/replayUrl";
import { FOOTER, KIND_LABEL, dotClass, evidencePath, evidenceWord, whenFormat } from "../lib/replayView";
import { RLink } from "./common";

/** The severity a dot shows, for a screen reader; the kind already says Kubernetes, resolved or gap. */
const SEVERITY_WORD: Record<string, string> = { critical: "critical", warning: "warning", info: "info" };

/** The step's time, or its span as two times, the end on its own line. */
function When({ s, fmt }: { s: ReplayStep; fmt: (t: number) => string }) {
  const start = <time dateTime={momentIso(s.time)}>{fmt(s.time)}</time>;
  if (s.end !== undefined && s.end !== s.time) {
    return (
      <>
        {start}
        <span className="rp-to">
          –<time dateTime={momentIso(s.end)}>{fmt(s.end)}</time>
        </span>
      </>
    );
  }
  if (s.openAtB) {
    return (
      <>
        {start}
        <span className="rp-to">–still at B</span>
      </>
    );
  }
  return start;
}

function Step({ s, fmt }: { s: ReplayStep; fmt: (t: number) => string }) {
  const dot = dotClass(s);
  const more = s.evidenceCount - s.evidence.length;
  return (
    <li className={`rp-step k-${s.kind}${s.kind === "gap" ? " gap" : ""}`} data-step={s.id}>
      <span className="rp-when mono">
        <When s={s} fmt={fmt} />
      </span>
      <span className="rp-rail" aria-hidden="true">
        <span className={`rp-dot ${dot}`} />
        <span className="rp-conn" />
      </span>
      <div className="rp-what">
        <div className="rp-title">
          <span className="sr-only">{[KIND_LABEL[s.kind], SEVERITY_WORD[dot]].filter(Boolean).join(", ")}: </span>
          {s.title}
          {s.outsideRange && <span className="rp-tag">outside the range</span>}
        </div>
        {s.body && <p className="rp-body">{s.body}</p>}
        {s.evidence.length > 0 && (
          <ul className="rp-ev" aria-label="Evidence">
            {s.evidence.map((e, i) => {
              const p = evidencePath(e);
              return (
                <li key={`${e.kind}:${e.id}:${i}`}>
                  <span className="rp-evk">{evidenceWord(e)}</span>{" "}
                  {p ? (
                    <RLink to={p} className="x">
                      {e.label || e.id}
                    </RLink>
                  ) : (
                    e.label || e.id
                  )}
                </li>
              );
            })}
            {more > 0 && <li className="rp-more">and {more} more</li>}
          </ul>
        )}
      </div>
    </li>
  );
}

export function ReplaySteps({ steps, moments, notes }: { steps: ReplayStep[]; moments: Moments; notes: string[] }) {
  const fmt = whenFormat(moments);
  // Times with a day need a wider column.
  const list = `rp-list${fmt(moments.a).length > 8 ? " days" : ""}`;
  const inRange = steps.filter((s) => !s.outsideRange);
  const after = steps.filter((s) => s.outsideRange);
  return (
    <section className="sec rp-steps">
      <div className="sec-h">
        <h2>What happened, in order</h2>
      </div>
      <div className="card">
        {notes.length > 0 && (
          <div className="rp-notes" role="note">
            {notes.map((n) => (
              <p key={n}>{n}</p>
            ))}
          </div>
        )}
        {inRange.length === 0 ? (
          <p className="rp-empty">Nothing that nats-lens observed changed between A and B.</p>
        ) : (
          <ol className={list} aria-label="What happened, in order">
            {inRange.map((s) => (
              <Step key={s.id} s={s} fmt={fmt} />
            ))}
          </ol>
        )}
        {after.length > 0 && (
          <>
            <h3 className="rp-after" id="rp-after-b">
              After B
            </h3>
            <ol className={list} aria-labelledby="rp-after-b">
              {after.map((s) => (
                <Step key={s.id} s={s} fmt={fmt} />
              ))}
            </ol>
          </>
        )}
        <p className="rp-foot">{FOOTER}</p>
      </div>
    </section>
  );
}
