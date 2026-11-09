// The path the UI is served under. natslens serves it at the root; the demo
// site on GitHub Pages serves it under the repository's name, set at build
// time (Vite's base, from NATSLENS_BASE). The router takes it as its
// basename, so a <Link> or navigate() keeps it; a URL built by hand from a
// route path goes through appHref or appUrl.

/** The base path without a trailing slash: "" at the root, "/nats-lens" on the demo site. */
export function basePath(): string {
  return import.meta.env.BASE_URL.replace(/\/+$/, "");
}

/** A route path ("/servers?range=1h") as the browser's path, with the base path. */
export function appHref(path: string): string {
  return basePath() + path;
}

/** A route path as an absolute URL, for a link copied or saved outside the page. */
export function appUrl(path: string): string {
  return window.location.origin + appHref(path);
}

/**
 * The router's basename: the base path with its trailing slash, so that a
 * link to the root ends in "/" ("/nats-lens/?system=…"), the address the
 * site serves without a redirect.
 */
export function routerBasename(): string {
  return import.meta.env.BASE_URL;
}

/**
 * Gives the address of the base path itself ("/nats-lens") its trailing
 * slash, which the router's basename needs to match it.
 */
export function slashBaseAddress(): void {
  const base = basePath();
  if (base && window.location.pathname === base) {
    window.history.replaceState(window.history.state, "", `${base}/${window.location.search}${window.location.hash}`);
  }
}
