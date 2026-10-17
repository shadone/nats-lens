// Theme switch: system, then light, then dark. The choice is kept in
// localStorage under the key the head script in index.html reads before
// first paint; "system" is stored as no key at all.

export type Theme = "system" | "light" | "dark";

export const THEMES: Theme[] = ["system", "light", "dark"];
export const THEME_KEY = "nats-lens-theme";

export function storedTheme(): Theme {
  try {
    const v = localStorage.getItem(THEME_KEY);
    return v === "light" || v === "dark" ? v : "system";
  } catch {
    return "system";
  }
}

export function applyTheme(t: Theme, root: HTMLElement = document.documentElement): void {
  if (t === "system") root.removeAttribute("data-theme");
  else root.setAttribute("data-theme", t);
  try {
    if (t === "system") localStorage.removeItem(THEME_KEY);
    else localStorage.setItem(THEME_KEY, t);
  } catch {
    // Storage blocked: the choice lasts for this page only.
  }
}

export function nextTheme(t: Theme): Theme {
  return THEMES[(THEMES.indexOf(t) + 1) % THEMES.length];
}

/** Icon fill: half for system, none for light, full for dark. */
export function themeIconPath(t: Theme): string {
  if (t === "light") return "";
  if (t === "dark") return "M7 1.4a5.6 5.6 0 1 1 0 11.2a5.6 5.6 0 1 1 0-11.2z";
  return "M7 1.4a5.6 5.6 0 0 1 0 11.2z";
}
