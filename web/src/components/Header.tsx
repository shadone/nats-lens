// Top bar of every page: the page's name, the account scope on the pages
// that apply it, search, the time range with freshness, and the theme. At
// phone width a menu button opens the sidebar as a drawer.
import { useLocation } from "react-router";
import { useShellStatus } from "../api/queries";
import { navFor } from "../nav";
import { AccountScope } from "./AccountScope";
import { RangeNotice, RangePicker } from "./RangePicker";
import { SearchJump } from "./SearchJump";
import { ThemeMenu } from "./ThemeMenu";

interface Props {
  menuOpen?: boolean;
  onMenu?: () => void;
}

export function Header({ menuOpen = false, onMenu }: Props) {
  const { pathname } = useLocation();
  const page = navFor(pathname);
  const status = useShellStatus();
  return (
    <header className="top">
      <div className="bar">
        <button type="button" className="menu-btn" aria-label="Menu" aria-expanded={menuOpen} aria-controls="sidebar" onClick={onMenu}>
          <svg viewBox="0 0 18 18" aria-hidden="true">
            <path d="M3 5h12M3 9h12M3 13h12" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
          </svg>
        </button>
        {page && <span className="page-title">{page.label}</span>}
        {page?.accountScope && pathname === page.path && <AccountScope />}
        <span className="bar-gap" />
        <SearchJump />
        <RangePicker freshness={status.data?.freshness} error={status.error ?? undefined} />
        <ThemeMenu />
      </div>
      <RangeNotice />
    </header>
  );
}
