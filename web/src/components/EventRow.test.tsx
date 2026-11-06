import { fireEvent, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { LensEvent, Finding } from "../api/model";
import { eventPath } from "../lib/view";
import { renderApp } from "../test/render";
import { EventRow, repeatText } from "./EventRow";

const awkward = "nats:A/s?x=1&range=7d #2";

const ev: LensEvent = {
  id: awkward,
  time: Date.now() - 60_000,
  source: "nats",
  kind: "consumer.deleted",
  type: "CONSUMER.DELETED",
  summary: "c on s",
  server: "",
  account: "A",
  stream: "s",
  consumer: "c",
  clientName: "",
  clientHost: "",
  reason: "",
  raised: [],
  raw: "",
};

describe("event links", () => {
  it("encode the event id so it reads back whole", () => {
    expect(eventPath(awkward)).toBe("/events?id=nats%3AA%2Fs%3Fx%3D1%26range%3D7d%20%232");
    const url = new URL(eventPath(awkward), "http://x");
    expect(url.searchParams.get("id")).toBe(awkward);
    expect(url.searchParams.has("range")).toBe(false);
  });

  it("keep the whole id in the row's link", () => {
    renderApp(<EventRow e={ev} end={Date.now()} open onToggle={() => {}} />, "/events?range=6h");
    const href = screen.getByRole("link", { name: "Link to this event" }).getAttribute("href") ?? "";
    const url = new URL(href, "http://x");
    expect(url.pathname).toBe("/events");
    expect(url.searchParams.get("id")).toBe(awkward);
    expect(url.searchParams.get("range")).toBe("6h");
  });
});

describe("repeat text", () => {
  const t = Date.UTC(2026, 9, 17, 9, 0);
  it("says nothing for one event", () => {
    expect(repeatText(1, t - 60_000, t)).toBeUndefined();
    expect(repeatText(undefined, undefined, t)).toBeUndefined();
  });
  it("gives the count and the span", () => {
    expect(repeatText(5, t - 6 * 60_000, t)).toBe("×5 in 6 min");
    expect(repeatText(3, t - 40_000, t)).toBe("×3 in 40 s");
    expect(repeatText(9, t - 2 * 3_600_000, t)).toBe("×9 in 2h");
  });
  it("gives the count alone when the events share one time", () => {
    expect(repeatText(4, t, t)).toBe("×4");
  });
});


describe("event text selection", () => {
  function setup() {
    const toggle = vi.fn();
    renderApp(<><p>Elsewhere</p><EventRow e={ev} end={Date.now()} open={false} onToggle={toggle} /></>);
    const button = screen.getByRole("button", { name: /CONSUMER.DELETED/ });
    const select = (node: Node) => {
      const range = document.createRange();
      range.selectNodeContents(node);
      window.getSelection()!.removeAllRanges();
      window.getSelection()!.addRange(range);
    };
    return { toggle, button, select };
  }

  it("keeps a row unchanged when a pointer click ends a selection in its summary", () => {
    const { toggle, button, select } = setup();
    select(screen.getByText(ev.summary));
    fireEvent.click(button, { detail: 1 });
    expect(toggle).not.toHaveBeenCalled();
    window.getSelection()!.removeAllRanges();
    fireEvent.click(button, { detail: 1 });
    expect(toggle).toHaveBeenCalledOnce();
  });

  it("allows keyboard activation with a selection in this row", () => {
    const { toggle, button, select } = setup();
    select(screen.getByText(ev.summary));
    fireEvent.click(button, { detail: 0 });
    expect(toggle).toHaveBeenCalledOnce();
  });

  it("allows pointer activation with a stale selection elsewhere", () => {
    const { toggle, button, select } = setup();
    select(screen.getByText("Elsewhere"));
    fireEvent.click(button, { detail: 1 });
    expect(toggle).toHaveBeenCalledOnce();
  });
});


describe("check references in events", () => {
  const finding: Finding = { id:"consumer-009:APP/order-jobs/resize-worker",checkCode:"consumer-009",checkName:"Consumer creation time differs across servers",severity:"info",state:"firing",detail:"Timestamps differ",remediation:"",relatedConfig:[],relatedConfigSource:"",transitionsLastHour:1 };
  for (const source of ["finding","nats"] as const) {
    it(`links ${source} event associations without dropping context`, () => {
      renderApp(<EventRow e={{...ev,source,raised:source==="nats"?[finding]:[],finding:source==="finding"?finding:undefined}} end={Date.now()} open onToggle={() => {}} />,"/events?system=example-prod&range=6h&account=APP");
      const url=new URL(screen.getByRole("link",{name:"consumer-009 · Check reference"}).getAttribute("href")!,"http://example.com");
      expect(url.pathname).toBe("/checks/consumer-009");
      expect(Object.fromEntries(url.searchParams)).toEqual({range:"6h",system:"example-prod",account:"APP"});
    });
  }
});
