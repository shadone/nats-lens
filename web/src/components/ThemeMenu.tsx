// The theme: one button with the current theme's icon and a chevron, which
// opens Light, Dark and System with a check on the current one.
import { useState } from "react";
import { applyTheme, storedTheme, THEME_ICON, THEME_LABEL, THEMES, type Theme } from "../lib/theme";
import { Menu } from "./Menu";

function ThemeIcon({ t }: { t: Theme }) {
  return (
    <svg className="ti" viewBox="0 0 14 14" aria-hidden="true">
      <path d={THEME_ICON[t]} fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

export function Chevron() {
  return (
    <svg className="chev" viewBox="0 0 10 10" aria-hidden="true">
      <path d="M2 3.5 5 6.5 8 3.5" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

export function ThemeMenu() {
  const [theme, setTheme] = useState<Theme>(storedTheme);
  const label = `Theme: ${THEME_LABEL[theme]}`;
  return (
    <Menu
      label={label}
      title={label}
      menuLabel="Theme"
      className="theme"
      choices={THEMES.map((t) => ({
        key: t,
        label: THEME_LABEL[t],
        icon: <ThemeIcon t={t} />,
        checked: t === theme,
        onSelect: () => {
          applyTheme(t);
          setTheme(t);
        },
      }))}
    >
      <ThemeIcon t={theme} />
      <Chevron />
    </Menu>
  );
}
