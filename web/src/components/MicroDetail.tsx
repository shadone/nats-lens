// The chosen service under the cards: its endpoint table with the board's
// columns (a slow endpoint's row in the warning colours), its instances
// with their connections and the missing ones, or, for a service seen
// only in subscriptions, what is known and how to see more.
import type { MicroAccount, MicroEndpoint, MicroService } from "../api/micro";
import { cutText, missingText, msText, noEndpointsText, rateText, seenOnlyText, shareText, slowText, whenText } from "../lib/microView";
import { serverPath } from "../lib/view";
import { Dash, RLink } from "./common";
import { connectionPath } from "./SubjectsTable";

const enc = encodeURIComponent;

/** A subject without wildcards opens the Subjects page's tester on it, in the service's account. */
export function subjectPath(account: string, subject: string): string | undefined {
  if (subject === "" || subject.split(".").some((t) => t === "*" || t === ">")) return undefined;
  return `/subjects?account=${enc(account)}&subject=${enc(subject)}`;
}

function Subject({ account, subject }: { account: string; subject: string }) {
  const to = subjectPath(account, subject);
  return to ? <RLink to={to}>{subject}</RLink> : <>{subject}</>;
}

/** No value: the cell's dash, heard as the words given. */
function None({ words }: { words: string }) {
  return (
    <span className="mi-quiet">
      <span aria-hidden="true">–</span>
      <span className="sr-only">{` ${words}`}</span>
    </span>
  );
}

function EndpointRow({ s, e, epochAt }: { s: MicroService; e: MicroEndpoint; epochAt: number }) {
  return (
    <tr className={e.slow ? "mi-warn" : undefined}>
      <th scope="row">{e.name}</th>
      <td className="mi-subj">
        <Subject account={s.account} subject={e.subject} />
      </td>
      <td className="mi-subj">{e.queueGroup || <None words="no queue group" />}</td>
      <td className="num">{e.requestsPerSecond === undefined ? <Dash /> : rateText(e.requestsPerSecond)}</td>
      <td className={`num${e.slow ? " mi-warn-text" : ""}`}>{e.averageMs === undefined ? <Dash /> : msText(e.averageMs)}</td>
      <td className={`num${e.errorsHigh ? " mi-crit" : ""}`}>{e.errorShare === undefined ? <Dash /> : shareText(e.errorShare)}</td>
      <td className="mi-err">
        {e.slow && <span className="mi-line mi-warn-text">{slowText(e, epochAt)}</span>}
        {e.lastError ? (
          <span className="mi-line">
            {e.lastError}
            {e.lastErrorInstance && <span className="mi-by"> on instance {e.lastErrorInstance}</span>}
          </span>
        ) : (
          !e.slow && <None words="no error reported" />
        )}
      </td>
    </tr>
  );
}

function Endpoints({ s, accounts, epochAt }: { s: MicroService; accounts: MicroAccount[]; epochAt: number }) {
  if (s.endpoints.length === 0) return <p className="mi-foot">{noEndpointsText(s, accounts)}</p>;
  const cut = cutText(s.endpoints.length, s.endpointsTotal, "endpoints");
  return (
    <>
      <div className="table-wrap scroll-cue">
        <table className="tbl mi-tbl">
          <thead>
            <tr>
              <th scope="col">Endpoint</th>
              <th scope="col">Subject</th>
              <th scope="col">Queue group</th>
              <th scope="col" className="num">
                Req/s
              </th>
              <th scope="col" className="num">
                Avg time
              </th>
              <th scope="col" className="num">
                Errors
              </th>
              <th scope="col">Last error</th>
            </tr>
          </thead>
          <tbody>
            {s.endpoints.map((e) => (
              <EndpointRow key={e.name} s={s} e={e} epochAt={epochAt} />
            ))}
          </tbody>
        </table>
      </div>
      {cut && <p className="mi-foot">{cut}</p>}
    </>
  );
}

function Connection({ name }: { name: string }) {
  return name ? <RLink to={connectionPath(name)}>{name}</RLink> : <None words="not found" />;
}

function Instances({ s, epochAt }: { s: MicroService; epochAt: number }) {
  const cut = cutText(s.instanceList.length, s.instances, "instances");
  if (s.instanceList.length === 0 && s.missing.length === 0) return null;
  return (
    <>
      <h3 className="mi-h3">Instances</h3>
      <div className="table-wrap scroll-cue">
        <table className="tbl mi-tbl mi-inst">
          <thead>
            <tr>
              <th scope="col">Instance</th>
              <th scope="col">Version</th>
              <th scope="col">Started</th>
              <th scope="col">Connection</th>
              <th scope="col">Server</th>
              <th scope="col">Note</th>
            </tr>
          </thead>
          <tbody>
            {s.instanceList.map((i) => (
              <tr key={i.id}>
                <th scope="row">{i.id}</th>
                <td className="mi-subj">{i.version || <None words={s.asked ? "not reported" : "not asked"} />}</td>
                <td className="mi-when">{i.started !== undefined ? whenText(i.started, epochAt) : <None words={s.asked ? "not reported" : "not asked"} />}</td>
                <td className="mi-subj">
                  <Connection name={i.connectionName} />
                </td>
                <td className="mi-subj">{i.server ? <RLink to={serverPath(i.server)}>{i.server}</RLink> : <None words="not found" />}</td>
                <td className="mi-note-cell" />
              </tr>
            ))}
            {s.missing.map((m) => (
              <tr key={`missing:${m.id}`} className="mi-warn">
                <th scope="row">{m.id}</th>
                <td>
                  <None words="not known" />
                </td>
                <td>
                  <None words="not known" />
                </td>
                <td className="mi-subj">
                  <Connection name={m.connectionName} />
                </td>
                <td>
                  <None words="not known" />
                </td>
                <td className="mi-note-cell mi-warn-text">{missingText(m, s.asked, epochAt)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {cut && <p className="mi-foot">{cut}</p>}
    </>
  );
}

export function MicroDetail({
  s,
  accounts,
  epochAt,
  accountLabel,
  showAccount,
}: {
  s: MicroService;
  accounts: MicroAccount[];
  epochAt: number;
  accountLabel: string;
  showAccount: boolean;
}) {
  const id = "mi-detail-h";
  return (
    <section className="card mi-detail" aria-labelledby={id}>
      <h2 id={id} className="mi-h2">
        <span className="mono">{s.name}</span> {s.asked ? "endpoints" : "service"}
        {showAccount && <span className="mi-in"> in {accountLabel}</span>}
      </h2>
      {s.asked ? (
        <Endpoints s={s} accounts={accounts} epochAt={epochAt} />
      ) : (
        <div className="mi-seen">
          {seenOnlyText(s, accountLabel).map((l) => (
            <p key={l}>{l}</p>
          ))}
        </div>
      )}
      <Instances s={s} epochAt={epochAt} />
    </section>
  );
}
