// The Capacity page's account cards: each account JWT's JetStream limit
// for file or memory storage, the account's use against it, its growth and
// when it fills. The limit is shared by every stream of the account.
import type { AccountStore } from "../api/capacity";
import { accountAsStore, accountStoreTitle, growthLine, storeUse } from "../lib/capacityView";
import { fmtBytes } from "../lib/format";
import { accountLabel } from "../lib/view";
import { RLink } from "./common";

function AccountCard({ a }: { a: AccountStore }) {
  const s = accountAsStore(a);
  const use = storeUse(s);
  const label = accountLabel(a.account, a.accountName || undefined);
  const growth = s.perDay === undefined && s.reported ? <span title="This account lacks usable observations spanning 6 h">growth unavailable</span> : growthLine(s);
  return (
    <section className="card cap-server" aria-label={`Account ${label} ${a.storage} limit`}>
      <div className="cap-server-h">
        <RLink to="/accounts" className="x cap-server-name" title={a.account}>
          {label}
        </RLink>
        <span className="cap-store">{accountStoreTitle(a)}</span>
      </div>
      {!use ? (
        <p className="cap-growth cap-quiet">limit {fmtBytes(a.limit)}; use not reported in this collection</p>
      ) : (
        <>
          <div className="cap-use">
            {use.used} <span className="cap-of">{use.of}</span>
          </div>
          {use.pct !== undefined && (
            <div className="cap-bar" aria-hidden="true">
              <div className={`cap-bar-fill${use.pct > 0 ? " cap-bar-some" : ""}`} style={{ width: `${use.pct}%` }} />
            </div>
          )}
          {growth && <p className="cap-growth">{growth}</p>}
        </>
      )}
    </section>
  );
}

export function AccountCards({ accounts }: { accounts: AccountStore[] }) {
  if (accounts.length === 0) return null;
  return (
    <section className="cap-servers cap-group" aria-label="Account limits">
      <h2 className="cap-section">Account limits</h2>
      <p className="cap-note cap-section-note">
        Each limit is the account JWT's and is shared by all the account's streams of that storage. Use counts every replica, the largest any server reports for the account. Streams without a limit of their own name it as their bound.
      </p>
      <div className="cap-cards">
        {accounts.map((a) => (
          <AccountCard key={`${a.account}/${a.storage}`} a={a} />
        ))}
      </div>
    </section>
  );
}
