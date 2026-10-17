import { RLink } from "../components/common";
import { usePageTitle } from "../lib/usePageTitle";

export function NotFound() {
  usePageTitle("Not found");
  return (
    <div className="page-h">
      <h1>Page not found</h1>
      <span className="aside">
        Nothing lives at this address. <RLink to="/">Back to the overview</RLink>.
      </span>
    </div>
  );
}
