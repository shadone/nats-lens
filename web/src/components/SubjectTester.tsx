// "Where would a message go?": the subject field beside the filters and
// the card under them with what a message on the typed subject would
// reach, by account. Only the answer for the subject in the field now is
// shown; an answer for a subject typed before never stands in.
import { Code, ConnectError } from "@connectrpc/connect";
import { Fragment } from "react";
import type { StreamMatch, SubjectMatches } from "../api/subjects";
import { consumersText, coreLead, invalidReason, matchGroups, NOTHING_REACHED } from "../lib/subjectsView";
import { consumerPath, streamPath } from "../lib/view";
import { AccountName } from "./AccountName";
import { errorText, RLink } from "./common";
import { Names } from "./SubjectsTable";

/** The server's reason when it refused the subject, else undefined. */
export function refusal(e: unknown): string | undefined {
  return e instanceof ConnectError && e.code === Code.InvalidArgument ? invalidReason(e.rawMessage) : undefined;
}

export const WHY_ID = "sj-why";

export function SubjectField({ value, onChange, why }: { value: string; onChange: (v: string) => void; why?: string }) {
  return (
    <div className="sj-field-wrap">
      <label className={`sj-field${why ? " sj-field-bad" : ""}`}>
        <span className="mono sj-field-k" aria-hidden="true">
          subject
        </span>
        <input
          type="text"
          className="mono"
          aria-label="Test a subject"
          placeholder="orders.place"
          spellCheck={false}
          autoComplete="off"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          aria-invalid={why ? true : undefined}
          aria-describedby={why ? WHY_ID : undefined}
        />
      </label>
      {why && (
        <p id={WHY_ID} className="sj-why" role="alert">
          {why}
        </p>
      )}
    </div>
  );
}

function ConsumerNames({ s }: { s: StreamMatch }) {
  return (
    <>
      {s.consumers.map((c, i) => (
        <Fragment key={c}>
          {i > 0 && (i === s.consumers.length - 1 && s.ephemeral === 0 ? " and " : ", ")}
          <RLink to={consumerPath(s.account, s.stream, c)} className="x mono">
            {c}
          </RLink>
        </Fragment>
      ))}
      {s.ephemeral > 0 && `${s.consumers.length ? " and " : ""}${s.ephemeral} ephemeral`}
    </>
  );
}

function StreamReach({ s }: { s: StreamMatch }) {
  const consumers = consumersText(s);
  return (
    <>
      <span className="sj-chip sj-chip-stream">
        <span className="sj-k">Stream</span>{" "}
        <RLink to={streamPath(s.account, s.stream)} className="x mono">
          {s.stream}
        </RLink>{" "}
        <span className="sj-via">
          via <span className="mono">{s.pattern}</span>
        </span>
      </span>
      <span className={`sj-chip${consumers ? "" : " sj-chip-warn"}`}>
        {consumers ? (
          <>
            <span className="sj-k">then consumers</span> <ConsumerNames s={s} />
          </>
        ) : (
          <span className="sj-k">no consumer reads it</span>
        )}
      </span>
      {s.mirrors.map((m) => (
        <span key={`m:${m}`} className="sj-chip">
          <span className="sj-k">Mirror</span>{" "}
          <RLink to={streamPath(s.account, m)} className="x mono">
            {m}
          </RLink>
        </span>
      ))}
      {s.sourcedBy.map((m) => (
        <span key={`s:${m}`} className="sj-chip">
          <span className="sj-k">Sourced by</span>{" "}
          <RLink to={streamPath(s.account, m)} className="x mono">
            {m}
          </RLink>
        </span>
      ))}
    </>
  );
}

/** What the card says, from the answer: by account, unless the page shows one. */
function Reach({ m, scoped }: { m: SubjectMatches; scoped: boolean }) {
  const groups = matchGroups(m);
  if (groups.length === 0) {
    return (
      <p className="sj-reach-none">
        {m.subscriptionsAt === undefined ? "no stream stores it; subscriptions were not collected, so subscribers are not known" : NOTHING_REACHED}
      </p>
    );
  }
  return (
    <>
      {groups.map((g) => (
        <div key={g.account} className="sj-group" role="group" aria-label={`Account ${g.accountName || g.account}`}>
          {!scoped && (
            <div className="sj-group-h">
              <span className="sj-k">Account</span> <AccountName id={g.account} name={g.accountName || undefined} />
            </div>
          )}
          <div className="sj-chips">
            {g.streams.map((s) => (
              <StreamReach key={s.stream} s={s} />
            ))}
            {g.core.length > 0 ? (
              <span className="sj-chip">
                <span className="sj-k">Core subscribers</span>{" "}
                {g.core.map((c, i) => (
                  <Fragment key={`${c.subject}|${c.queue}`}>
                    {i > 0 && "; "}
                    {coreLead(c)}
                    <Names names={c.names} more={c.moreNames} /> via <span className="mono">{c.subject}</span>
                  </Fragment>
                ))}
              </span>
            ) : (
              <span className="sj-chip">
                <span className="sj-k">Core subscribers</span> {m.subscriptionsAt === undefined ? "not collected" : "none"}
              </span>
            )}
          </div>
        </div>
      ))}
    </>
  );
}

/**
 * subject: the subject the answer is for. answer: undefined while it loads.
 * error: a failure other than a refused subject, which the field shows.
 */
export function ReachCard({ subject, answer, error, scoped }: { subject: string; answer?: SubjectMatches; error?: unknown; scoped: boolean }) {
  return (
    <section className="card sj-reach" aria-label="Where this subject goes">
      <div className="sj-k sj-reach-h">
        A message on <span className="mono sj-subject">{subject}</span> would reach
      </div>
      {error ? (
        <p className="sj-reach-none" role="alert">
          Could not look it up: {errorText(error)}
        </p>
      ) : answer === undefined ? (
        <p className="sj-reach-none" role="status">
          Looking it up…
        </p>
      ) : (
        <Reach m={answer} scoped={scoped} />
      )}
    </section>
  );
}
