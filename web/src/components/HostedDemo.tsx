// What only the demo site shows: the start while its API downloads, and a
// line that says the data is made up. See lib/hostedDemo.ts.
import { Loading } from "./common";

export const REPOSITORY_URL = "https://github.com/shadone/nats-lens";

/** The page while the demo's WebAssembly downloads and starts, before the shell. */
export function DemoStarting() {
  return (
    <div className="demo-starting">
      <Loading what="the demo" />
      <p>It runs in your browser; the first visit downloads about 5 MB.</p>
    </div>
  );
}

/** The line above the top bar of every page of the demo site. */
export function DemoNotice() {
  return (
    <p className="demo-notice">
      Demo with made-up data, generated in your browser. Nothing is collected from a real NATS system.{" "}
      <a className="x" href={REPOSITORY_URL} rel="noreferrer">
        nats-lens on GitHub
      </a>
    </p>
  );
}
