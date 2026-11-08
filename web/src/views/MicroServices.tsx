import type { MicroServicesModel } from "../pages/MicroServices";
// Micro services: the services built on the NATS micro framework in the
// range's newest collection, a card each, and the chosen card's endpoints
// and instances. The chosen service is kept in the address as
// ?service=<account>/<name>.
import { useSearchParams } from "react-router";
import type { MicroServices as Micro, MicroAccount } from "../api/micro";
import { AccountName } from "../components/AccountName";
import { Empty, QueryView } from "../components/common";
import { MicroCards } from "../components/MicroCards";
import { MicroDetail } from "../components/MicroDetail";
import { Words } from "../components/Words";
import { hms } from "../lib/format";
import { chooseService, footLines, MICRO_FOOTNOTE, MICRO_SUBTITLE, NO_SUBSCRIPTIONS, noServiceText, serviceKey, unansweredText, unknownServiceText, withAccountNames } from "../lib/microView";
import { accountLabeller } from "../lib/view";


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

function Body({ d, scope, names, serviceParam }: { d: Micro; scope: string; names: Map<string, string>; serviceParam: string }) {
  const [params, setParams] = useSearchParams();
  const label = accountLabeller([...d.services, ...d.accounts]);
  const accountText = (a: Pick<MicroAccount, "account" | "accountName">) => label(a.account, a.accountName || undefined);
  const nameOf = (id: string) => [...d.accounts, ...d.services].find((a) => a.account === id && a.accountName)?.accountName || names.get(id) || undefined;
  const idText = (id: string) => label(id, nameOf(id));
  const scopeLabel = scope ? idText(scope) : "";
  const param = params.get(serviceParam);
  const { chosen, unknown } = chooseService(d.services, param, scope);
  const epochAt = d.epochAt ?? 0;
  const showAccount = scope === "" && new Set(d.services.map((s) => s.account)).size > 1;
  const unanswered = d.accounts.map((a) => ({ a, text: unansweredText(a, d.epochAt ?? 0) })).filter((x) => x.text);

  const choose = (key: string) =>
    setParams(
      (p) => {
        const n = new URLSearchParams(p);
        n.set(serviceParam, key);
        return n;
      },
      { replace: true },
    );

  return (
    <>
      {/* With nothing asked and no service, the empty state says it alone. */}
      {d.subscriptionsAt === undefined && (d.services.length > 0 || d.accounts.some((a) => a.asked)) && (
        <p className="mi-notice" role="note">
          <b>Subscriptions not collected.</b> <Words p={NO_SUBSCRIPTIONS} />
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
        <Empty>
          <span className="mi-wrap">
            <Words p={noServiceText(d, scopeLabel)} />
          </span>
        </Empty>
      ) : (
        <>
          {unknown && (
            <p className="mi-notice" role="note">
              {unknownServiceText(param ?? "", scope, idText, chosen!.name)}
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
        <p>
          <Words p={MICRO_FOOTNOTE} />
        </p>
        {footLines(d, accountText).map((l) => (
          <p key={l}>{l}</p>
        ))}
      </div>
    </>
  );
}

export function MicroServicesView({ model }: { model: MicroServicesModel }) {
  const { serviceParam, account, q, names } = model;

  return (
    <>
      <div className="page-h">
        <Subtitle d={q.data} />
      </div>
      <QueryView q={q} what="micro services">
        {(d) => (d.collected ? <Body d={withAccountNames(d, names)} scope={account} names={names} serviceParam={serviceParam} /> : <Empty>No collection in this range.</Empty>)}
      </QueryView>
    </>
  );
}
