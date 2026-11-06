// Same boundary as responsive.css and app-responsive.css.
import { useSyncExternalStore } from "react";
const QUERY = "(max-width: 760px)";
function subscribe(changed: () => void) {
  const media = window.matchMedia?.(QUERY);
  media?.addEventListener("change", changed);
  return () => media?.removeEventListener("change", changed);
}
export function usePhone(): boolean {
  return useSyncExternalStore(subscribe, () => window.matchMedia?.(QUERY).matches ?? false, () => false);
}
