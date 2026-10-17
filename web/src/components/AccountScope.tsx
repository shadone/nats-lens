// The account scope in the header: all accounts, or one. It writes
// ?account=, which links carry to the next page.
import { useAccounts } from "../api/systems";
import { useAccountScope } from "../lib/useSystem";
import { useTimeRange } from "../lib/useTimeRange";
import { Menu } from "./Menu";
import { Chevron } from "./ThemeMenu";

export function AccountScope() {
  const { spec } = useTimeRange();
  const [account, setAccount] = useAccountScope();
  const accounts = useAccounts(spec).data ?? [];
  const current = accounts.find((a) => a.account === account);
  const name = current?.name || account;
  const text = account ? `Account: ${name}` : "All accounts";
  return (
    <Menu
      label={text}
      menuLabel="Accounts"
      className="scope"
      align="left"
      choices={[
        { key: "", label: accounts.length ? `All accounts (${accounts.length})` : "All accounts", checked: !account, onSelect: () => setAccount("") },
        ...accounts.map((a) => ({
          key: a.account,
          label: <span className="mono">{a.name || a.account}</span>,
          note: a.isSystem ? "system" : undefined,
          checked: a.account === account,
          onSelect: () => setAccount(a.account),
        })),
      ]}
    >
      {account ? (
        <>
          <span className="k">Account:</span>{" "}
          <b className="mono" title={account}>
            {name}
          </b>
        </>
      ) : (
        <span>All accounts</span>
      )}
      <Chevron />
    </Menu>
  );
}
