// Theme: light, dark or system. The choice is kept in
// localStorage under the key the head script in index.html reads before
// first paint; "system" is stored as no key at all.

export type Theme = "system" | "light" | "dark";

/** In the order of the theme menu. */
export const THEMES: Theme[] = ["light", "dark", "system"];

export const THEME_LABEL: Record<Theme, string> = { light: "Light", dark: "Dark", system: "System" };

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

/** Icon of each theme on a 14x14 grid, drawn as a 1.4 stroke: a sun, a moon, a screen. */
export const THEME_ICON: Record<Theme, string> = {
  light: "M7 1.5v1.2 M7 11.3v1.2 M1.5 7h1.2 M11.3 7h1.2 M3.1 3.1l.85.85 M10.05 10.05l.85.85 M3.1 10.9l.85-.85 M10.05 3.95l.85-.85 M7 4.6a2.4 2.4 0 1 0 0 4.8 2.4 2.4 0 0 0 0-4.8Z",
  dark: "M10.5 8.5A4.5 4.5 0 0 1 5.5 3.5a4.5 4.5 0 1 0 5 5Z",
  system: "M2 3h10v7H2Z M5 12.5h4",
};
