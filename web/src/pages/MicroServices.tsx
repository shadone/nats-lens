// Micro services: the services built on the NATS micro framework in the
// range's newest collection, a card each, and the chosen card's endpoints
// and instances. The chosen service is kept in the address as
// ?service=<account>/<name>.
import { useSearchParams } from "react-router";
import type { MicroAccount, MicroServices as Micro } from "../api/micro";
import { useMicroServices } from "../api/queries";
import { AccountName } from "../components/AccountName";
import { Empty, QueryView } from "../components/common";
import { MicroCards } from "../components/MicroCards";
import { MicroDetail } from "../components/MicroDetail";
import { hms } from "../lib/format";
import { chooseService, footLines, MICRO_FOOTNOTE, MICRO_SUBTITLE, noServiceText, serviceKey, unansweredText } from "../lib/microView";
import { usePageTitle } from "../lib/usePageTitle";
import { useAccountScope } from "../lib/useSystem";
import { useTimeRange } from "../lib/useTimeRange";
import { accountLabeller } from "../lib/view";

export const SERVICE_PARAM = "service";

function Subtitle({ d }: { d?: Micro }) {
  const [a, info, b, stats, end] = MICRO_SUBTITLE;
  return (
    <span className="aside">
      {a} <span className="mono">{info}</span> {b} <span className="mono">{stats}</span>
      {end}{" "}
      {d?.collected && d.epochAt !== undefined ? (
        <>
          In the collection of <span className="mono">{hms(d.epochAt)} UTC</span>.
        </>
      ) : (
        "In the newest collection of the range."
      )}
    </span>
  );
}

function Body({ d, scope }: { d: Micro; scope: string }) {
  const [params, setParams] = useSearchParams();
  const label = accountLabeller([...d.services, ...d.accounts]);
  const accountText = (a: Pick<MicroAccount, "account" | "accountName">) => label(a.account, a.accountName || undefined);
  const scopeAccount = d.accounts.find((a) => a.account === scope);
  const scopeLabel = scope ? (scopeAccount ? accountText(scopeAccount) : scope) : "";
  const param = params.get(SERVICE_PARAM);
  const { chosen, unknown } = chooseService(d.services, param);
  const epochAt = d.epochAt ?? 0;
  const showAccount = scope === "" && new Set(d.services.map((s) => s.account)).size > 1;
  const unanswered = d.accounts.map((a) => ({ a, text: unansweredText(a) })).filter((x) => x.text);

  const choose = (key: string) =>
    setParams(
      (p) => {
        const n = new URLSearchParams(p);
        n.set(SERVICE_PARAM, key);
        return n;
      },
      { replace: true },
    );

  return (
    <>
      {d.subscriptionsAt === undefined && (
        <p className="mi-notice" role="note">
          <b>Subscriptions not collected.</b> No subscription list within 24 hours before this collection: only the services of accounts listed under accounts are shown, and instances name no connection.
        </p>
      )}
      {unanswered.map(({ a, text }) => (
        <p key={a.account} className="mi-notice mi-notice-warn" role="note">
          <b>
            <AccountName id={a.account} name={a.accountName || undefined} label={accountText(a)} /> asked, no answer.
          </b>{" "}
          {text}
        </p>
      ))}
      {d.services.length === 0 ? (
        <Empty>{noServiceText(d, scopeLabel)}</Empty>
      ) : (
        <>
          {unknown && (
            <p className="mi-notice" role="note">
              No service {param} in this collection; showing {chosen!.name}.
            </p>
          )}
          <MicroCards
            services={d.services}
            chosen={chosen}
            accounts={d.accounts}
            epochAt={epochAt}
            showAccount={showAccount}
            label={label}
            onChoose={(s) => choose(serviceKey(s))}
          />
          {chosen && <MicroDetail s={chosen} accounts={d.accounts} epochAt={epochAt} accountLabel={label(chosen.account, chosen.accountName || undefined)} showAccount={showAccount} />}
        </>
      )}
      <div className="mi-note-foot">
        <p>{MICRO_FOOTNOTE}</p>
        {footLines(d, accountText).map((l) => (
          <p key={l}>{l}</p>
        ))}
      </div>
    </>
  );
}

export function MicroServices() {
  usePageTitle("Micro services");
  const { spec } = useTimeRange();
  const [account] = useAccountScope();
  const q = useMicroServices(spec, account);
  return (
    <>
      <div className="page-h">
        <Subtitle d={q.data} />
      </div>
      <QueryView q={q} what="micro services">
        {(d) => (d.collected ? <Body d={d} scope={account} /> : <Empty>No collection in this range.</Empty>)}
      </QueryView>
    </>
  );
}
