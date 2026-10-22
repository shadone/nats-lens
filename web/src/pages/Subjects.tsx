// Subjects: where messages can go in the range's newest collection: each
// stream with its subjects and consumers, the client subscriptions by
// subject, reply inboxes and the JetStream API, with a tester that says
// what a message on one subject would reach.
import { useState } from "react";
import { useSearchParams } from "react-router";
import { useMatchSubject, useSubjects } from "../api/queries";
import type { SubjectFilter } from "../api/subjects";
import { Empty, QueryView } from "../components/common";
import { ReachCard, refusal, SubjectField } from "../components/SubjectTester";
import { SubjectsTable } from "../components/SubjectsTable";
import { hms } from "../lib/format";
import { FILTERS, footLines, SUBJECTS_FOOTNOTE, SUBSCRIPTIONS_FOOTNOTE, SUBJECTS_SUBTITLE, subjectFilter } from "../lib/subjectsView";
import { useDebounced } from "../lib/useDebounced";
import { usePageTitle } from "../lib/usePageTitle";
import { useAccountScope } from "../lib/useSystem";
import { useTimeRange } from "../lib/useTimeRange";

/** How long the subject field stays still before it is looked up. */
export const SUBJECT_DEBOUNCE_MS = 300;

/** The subject a link puts in the tester's field. */
export const SUBJECT_PARAM = "subject";

function FilterChip({ label, n, on, onClick }: { label: string; n?: number; on: boolean; onClick: () => void }) {
  return (
    <button type="button" className={`chip facet${on ? " on" : ""}`} aria-pressed={on} aria-label={n === undefined ? label : `${label} ${n}`} onClick={onClick}>
      {label}
      {n !== undefined && <span className="n">{n}</span>}
    </button>
  );
}

export function Subjects() {
  usePageTitle("Subjects");
  const [params, setParams] = useSearchParams();
  const { spec } = useTimeRange();
  const [account] = useAccountScope();
  const filter = subjectFilter(params.get("filter"));
  const q = useSubjects(spec, account, filter);
  const d = q.data;

  // A link may fill the field once (?subject=, as the Micro services page
  // links an endpoint); typing drops the parameter, so the address never
  // names a subject the field no longer holds.
  const [typed, setTypedState] = useState(() => params.get(SUBJECT_PARAM) ?? "");
  const setTyped = (v: string) => {
    setTypedState(v);
    if (params.has(SUBJECT_PARAM))
      setParams(
        (p) => {
          const n = new URLSearchParams(p);
          n.delete(SUBJECT_PARAM);
          return n;
        },
        { replace: true },
      );
  };
  const subject = typed.trim();
  const asked = useDebounced(subject, SUBJECT_DEBOUNCE_MS);
  const mq = useMatchSubject(spec, account, asked);
  // The answer and the refusal count only for the subject in the field now.
  const current = subject !== "" && asked === subject;
  const why = current ? refusal(mq.error) : undefined;
  const failed = current && mq.error && !why ? mq.error : undefined;

  const setFilter = (f: SubjectFilter) =>
    setParams(
      (p) => {
        const n = new URLSearchParams(p);
        const param = FILTERS.find((x) => x.id === f)?.param ?? "";
        if (param) n.set("filter", param);
        else n.delete("filter");
        return n;
      },
      { replace: true },
    );

  return (
    <>
      <div className="page-h">
        <span className="aside">
          {SUBJECTS_SUBTITLE}{" "}
          {d?.collected && d.epochAt !== undefined ? (
            <>
              In the collection of <span className="mono">{hms(d.epochAt)} UTC</span>.
            </>
          ) : (
            "In the newest collection of the range."
          )}
        </span>
      </div>
      <div className="filters sj-filters">
        <div className="grp" role="group" aria-label="Filter">
          {FILTERS.map((f) => (
            <FilterChip key={f.id} label={f.label} n={d?.collected ? d.counts[f.id] : undefined} on={filter === f.id} onClick={() => setFilter(f.id)} />
          ))}
        </div>
        <SubjectField value={typed} onChange={setTyped} why={why} />
      </div>
      {/* Without a collection the page says so below; the card would say it again. */}
      {subject !== "" && !why && d?.collected !== false && (
        <ReachCard subject={subject} answer={current && !mq.isPending ? mq.data : undefined} error={failed} scoped={account !== ""} />
      )}
      <QueryView q={q} what="subjects">
        {(r) =>
          r.collected ? (
            <>
              {r.subscriptionsAt === undefined && (
                <p className="sj-missing" role="note">
                  <b>Subscriptions not collected.</b> No subscription list within 24 hours before this collection: the table lists stream subjects and the JetStream API only, and streams name no connections.
                </p>
              )}
              <SubjectsTable
                d={r}
                scoped={account !== ""}
                filterParam={FILTERS.find((f) => f.id === filter)?.param ?? ""}
                empty={filter === "all" ? "No stream or subscription in this collection." : `No pattern matches “${FILTERS.find((f) => f.id === filter)!.label}” in this collection.`}
              />
              <div className="sj-note-foot">
                <p>
                  {SUBJECTS_FOOTNOTE}
                  {r.subscriptionsAt !== undefined && ` ${SUBSCRIPTIONS_FOOTNOTE}`}
                </p>
                {footLines(r).map((l) => (
                  <p key={l}>{l}</p>
                ))}
              </div>
            </>
          ) : (
            <Empty>No collection in this range.</Empty>
          )
        }
      </QueryView>
    </>
  );
}
