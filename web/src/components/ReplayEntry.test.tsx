import { screen, fireEvent } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderApp } from "../test/render";
import type { Finding, Transition } from "../api/model";
import { ReplayEntry } from "./ReplayEntry";
const T = Date.parse("2026-10-17T12:00:00Z");
const f: Finding = { id: "f", checkCode: "server-004", checkName: "Restart", severity: "warning", state: "firing", since: T - 86400000, detail: "", remediation: "", relatedConfig: [], relatedConfigSource: "", transitionsLastHour: 1 };
const transition = (time: number, state: Transition["state"] = "firing"): Transition => ({time, state, severity: "warning", detail: ""});
function show(transitions: Transition[], finding = f) {
  return renderApp(<ReplayEntry finding={finding} transitions={transitions} start={T-3600000} end={T} now={T} minGapMs={30000} />, "/findings/f?range=6h&system=example-prod&account=APP&check=ignored");
}
function params() { return new URL(screen.getByRole("link", {name:"Replay this window"}).getAttribute("href")!, "http://example.com").searchParams; }
describe("finding Replay entry", () => {
  it("uses recorded transition context, not lifetime or condition start, and fixes the range", () => {
    show([transition(T-120000)], {...f, conditionSince:T-2*86400000});
    expect(Object.fromEntries(params())).toEqual({a:"2026-10-17T11:53:00Z",b:"2026-10-17T12:00:00Z",from:"2026-10-17T11:53:00Z",to:"2026-10-17T12:00:00Z",system:"example-prod"});
    expect(screen.getByText(/not the finding's duration/)).toBeInTheDocument();
  });
  it("selects historical transitions, ignoring a newer episode and an unsorted history", () => {
    show([transition(T+3600000), transition(T-1800000,"resolved"), transition(T-600000)], {...f,since:T+3600000});
    expect(params().get("a")).toBe("2026-10-17T11:45:00Z");
    expect(params().get("b")).toBe("2026-10-17T11:55:00Z");
  });
  it("uses a resolved observation when history is empty, without inventing a firing", () => {
    show([], {...f,state:"resolved",resolvedAt:T-600000});
    expect(params().get("a")).toBe("2026-10-17T11:45:00Z");
    expect(screen.getByText(/resolution/)).toBeInTheDocument();
  });
  it("does not use condition timing or a current finding to fabricate historical evidence", () => {
    show([], {...f,since:T+1000,conditionSince:T-600000});
    expect(screen.queryByRole("link",{name:"Replay this window"})).toBeNull();
    expect(screen.getByText(/No finding transition time is available in this range/)).toBeInTheDocument();
  });
  it("does not intercept modified clicks", () => {
    show([transition(T-600000)]);
    fireEvent.click(screen.getByRole("link",{name:"Replay this window"}), {ctrlKey:true});
    expect(screen.getByTestId("location")).toHaveTextContent("/findings/f?");
  });
});
