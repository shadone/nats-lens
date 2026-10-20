// Accounts: the accounts of a system in the range's newest collection with
// their limits, the failed logins of the 24 hours before it, and the
// certificates and JWTs that expire soon.
import { useAccountsAndAuth } from "../api/queries";
import { AccountsTable, ExpiringCard, FailedLoginsCard } from "../components/AccountsTables";
import { Empty, QueryView } from "../components/common";
import { headerLine } from "../lib/accountsView";
import { hms } from "../lib/format";
import { usePageTitle } from "../lib/usePageTitle";
import { useTimeRange } from "../lib/useTimeRange";

export function Accounts() {
  usePageTitle("Accounts");
  const { spec } = useTimeRange();
  const q = useAccountsAndAuth(spec);
  const d = q.data;
  return (
    <>
      <div className="page-h">
        <span className="aside">
          {d?.collected ? `${headerLine(d)}, ` : "Accounts, their limits, failed logins and what expires soon, "}
          {d?.epochAt !== undefined ? (
            <>
              in the collection of <span className="mono">{hms(d.epochAt)} UTC</span>
            </>
          ) : (
            "in the newest collection of the range"
          )}
          .
        </span>
      </div>
      <QueryView q={q} what="the accounts">
        {(r) =>
          r.collected ? (
            <>
              <AccountsTable d={r} />
              <div className="ac-cards">
                <FailedLoginsCard d={r} />
                <ExpiringCard d={r} />
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
