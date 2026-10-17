import { useEffect } from "react";

/** Sets the document title to "<page> · nats-lens". */
export function usePageTitle(page: string): void {
  useEffect(() => {
    document.title = page ? `${page} · nats-lens` : "nats-lens";
  }, [page]);
}
