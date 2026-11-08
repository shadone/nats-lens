import { Component, Suspense, type ReactNode } from "react";
import { Loading } from "./common";

/** Keep navigation available after a failed import. Reload clears the browser's
 * rejected module cache and obtains the current deployment's asset filenames. */
export class RouteBoundary extends Component<{ children: ReactNode; href: string }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() { return { failed: true }; }
  render() {
    if (this.state.failed) return <div className="state-box err" role="alert">
      <span>Could not load this page. Reload the page to try again.</span>
      <a className="btn" href={this.props.href}>Reload page</a>
    </div>;
    return <Suspense fallback={<Loading what="page" />}>{this.props.children}</Suspense>;
  }
}
