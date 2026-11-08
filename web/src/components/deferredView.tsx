import { useEffect, useState, type ComponentType } from "react";
import { Loading } from "./common";

/** Only presentation is deferred: the eager page owns its hooks and scope.
 * Explicit module state avoids mounting a new Suspense/lazy retry boundary.
 * A resolved view is synchronous on subsequent visits; data is never cached here.
 * Eager models spread query results before passing them here, subscribing to
 * errors/loading changes even when the view has not yet read their properties. */
export function deferredView<P extends object>(load: () => Promise<ComponentType<P>>) {
  let view: ComponentType<P> | undefined;
  let failure: unknown;
  let failed = false;
  let pending: Promise<void> | undefined;
  function start() {
    return pending ??= load().then(result => { view = result; }, error => {
      failure = error; failed = true;
    });
  }
  return function DeferredView(props: P) {
    const [, update] = useState(0);
    const View = view;
    const hasFailed = failed;
    useEffect(() => {
      if (View || hasFailed) return;
      let active = true;
      void start().then(() => { if (active) update(n => n + 1); });
      return () => { active = false; };
    }, [View, hasFailed]);
    // ASVS 16.5.1/16.5.2: RouteBoundary gives a generic error and retains
    // navigation. A native reload clears a rejected deployment module cache.
    if (hasFailed) throw failure;
    return View ? <View {...props} /> : <Loading what="page" />;
  };
}
