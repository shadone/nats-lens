// Theme button of the reference: cycles system, light, dark.
import { useState } from "react";
import { applyTheme, nextTheme, storedTheme, themeIconPath, type Theme } from "../lib/theme";

export function ThemeSwitch() {
  const [theme, setTheme] = useState<Theme>(storedTheme);
  const name = theme.charAt(0).toUpperCase() + theme.slice(1);
  const onClick = () => {
    const t = nextTheme(theme);
    applyTheme(t);
    setTheme(t);
  };
  return (
    <button className="theme" type="button" title="Theme: system, light or dark" aria-label={`Theme: ${name}. Change theme`} onClick={onClick}>
      <svg viewBox="0 0 14 14" aria-hidden="true">
        <circle cx="7" cy="7" r="5.6" fill="none" stroke="currentColor" strokeWidth="1.4" />
        <path d={themeIconPath(theme)} fill="currentColor" />
      </svg>
      <span className="k">Theme</span>
      <span>{name}</span>
    </button>
  );
}
