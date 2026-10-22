// The Micro services page's cards: one button per service with its name,
// versions, description, instances, requests per second, errors and note.
// The chosen card is pressed; choosing one shows its endpoints below.
import type { MicroAccount, MicroService } from "../api/micro";
import { descriptionText, noteOf, rateText, serviceErrorsHigh, serviceKey, shareText, versionText } from "../lib/microView";
import { AccountName } from "./AccountName";
import { Dash } from "./common";

/** A value a service seen only cannot have: nats-lens did not ask it. */
function NotAsked() {
  return (
    <span>
      <span aria-hidden="true">–</span>
      <span className="sr-only">{" not asked"}</span>
    </span>
  );
}

function Figure({ v, asked }: { v: string | undefined; asked: boolean }) {
  if (!asked) return <NotAsked />;
  return v === undefined ? <Dash /> : <>{v}</>;
}

function Card({
  s,
  chosen,
  accounts,
  epochAt,
  showAccount,
  label,
  onChoose,
}: {
  s: MicroService;
  chosen: boolean;
  accounts: MicroAccount[];
  epochAt: number;
  showAccount: boolean;
  label: (id: string, name?: string) => string;
  onChoose: () => void;
}) {
  const note = noteOf(s, accounts, epochAt);
  const version = versionText(s);
  const desc = descriptionText(s);
  const cls = ["mi-card", note.warn ? "mi-card-warn" : s.asked ? "" : "mi-card-seen", chosen ? "mi-card-on" : ""].filter(Boolean).join(" ");
  return (
    <button type="button" className={cls} aria-pressed={chosen} onClick={onChoose}>
      <span className="mi-card-h">
        <span className="mi-name">{s.name}</span>{" "}
        {version && (
          <span className="mi-ver">
            <span className="sr-only">version </span>
            {version}
          </span>
        )}
      </span>{" "}
      {desc && <span className={`mi-desc${s.asked ? "" : " mi-quiet"}`}>{desc}</span>}{" "}
      {showAccount && (
        <span className="mi-acc">
          <span className="sr-only">account </span>
          <AccountName id={s.account} name={s.accountName || undefined} label={label(s.account, s.accountName || undefined)} />
          {s.systemAccount && <span className="mi-system">system</span>}
        </span>
      )}{" "}
      <span className="mi-figs">
        <span className="mi-fig">
          <span className="mi-v">{s.instances}</span> <span className="mi-k">{s.instances === 1 ? "instance" : "instances"}</span>
        </span>{" "}
        <span className="mi-fig">
          <span className="mi-v">
            <Figure v={s.requestsPerSecond === undefined ? undefined : rateText(s.requestsPerSecond)} asked={s.asked} />
          </span>{" "}
          <span className="mi-k">req/s</span>
        </span>{" "}
        <span className="mi-fig">
          <span className={`mi-v${serviceErrorsHigh(s) ? " mi-crit" : ""}`}>
            <Figure v={s.errorShare === undefined ? undefined : shareText(s.errorShare)} asked={s.asked} />
          </span>{" "}
          <span className="mi-k">errors</span>
        </span>
      </span>{" "}
      <span className={`mi-note${note.warn ? " mi-note-warn" : ""}`}>{note.text}</span>
    </button>
  );
}

export function MicroCards({
  services,
  chosen,
  accounts,
  epochAt,
  showAccount,
  label,
  onChoose,
}: {
  services: MicroService[];
  chosen?: MicroService;
  accounts: MicroAccount[];
  epochAt: number;
  showAccount: boolean;
  label: (id: string, name?: string) => string;
  onChoose: (s: MicroService) => void;
}) {
  return (
    <div className="mi-cards" role="group" aria-label="Services">
      {services.map((s) => (
        <Card
          key={serviceKey(s)}
          s={s}
          chosen={chosen !== undefined && serviceKey(chosen) === serviceKey(s)}
          accounts={accounts}
          epochAt={epochAt}
          showAccount={showAccount}
          label={label}
          onChoose={() => onChoose(s)}
        />
      ))}
    </div>
  );
}
