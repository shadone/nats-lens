import { describe, expect, it } from "vitest";
import { segments } from "../charts/TrafficSpark";
import { app, dispatcher, hours, prod, resizer } from "../test/fixtures/applications";
import {
  APP_SORT,
  connectionsLink,
  countsText,
  emptyText,
  eventsLink,
  libraryCell,
  librariesText,
  mixedText,
  reconnectFinding,
  spreadText,
  subLine,
  trafficLabel,
  trafficNote,
} from "./appsView";
import { sortRows } from "./sort";

describe("appsView", () => {
  it("counts connections and applications, and says when the list was cut", () => {
    expect(countsText(prod())).toBe("51 connections, 3 applications");
    expect(countsText({ connectionsTotal: 1, applicationsTotal: 1, applications: [app()] })).toBe("1 connection, 1 application");
    expect(countsText({ connectionsTotal: 9000, applicationsTotal: 612, applications: Array(500).fill(app()) })).toBe("9,000 connections, 612 applications; the first 500 listed");
  });

  it("says what a restart of the busiest server moves only when the spread is concentrated", () => {
    expect(spreadText({ server: "nats-0", connections: 100, total: 187, servers: 3, concentrated: true })).toBe(
      "nats-0 serves 100 of 187 connections on 3 servers. A restart of nats-0 moves 100 of 187 connections at once.",
    );
    expect(spreadText({ server: "nats-eu-1", connections: 64, total: 1143, servers: 23, concentrated: false })).toBe("nats-eu-1 serves 64 of 1,143 connections on 23 servers. No server holds more than half.");
    expect(spreadText({ server: "nats-0", connections: 4, total: 4, servers: 1, concentrated: false })).toBe("Every connection is on nats-0: a restart of it moves all 4 connections at once.");
    expect(spreadText({ server: "", connections: 0, total: 0, servers: 0, concentrated: false })).toBe("No connection to spread.");
  });

  it("lists the libraries by the applications that use them, the rest counted", () => {
    expect(librariesText(prod().libraries)).toBe("nats.js 3.0.2 in 2 applications, go 1.54.0 in 1.");
    const many = ["1", "2", "3", "4", "5", "6"].map((v) => ({ lang: "go", version: `1.${v}`, applications: 1 }));
    expect(librariesText(many)).toBe("go 1.1 in 1 application, go 1.2 in 1, go 1.3 in 1, go 1.4 in 1, and 2 other versions.");
    expect(librariesText([{ lang: "", version: "", applications: 2 }])).toBe("library not reported in 2 applications.");
    expect(librariesText([])).toBe("No library reported.");
  });

  it("reads a row with more than one library version as mixed", () => {
    const libs = [
      { lang: "nats.js", version: "3.0.2", connections: 120 },
      { lang: "nats.js", version: "2.29.3", connections: 24 },
    ];
    expect(libraryCell(app({ libraries: libs }))).toEqual({ text: "mixed", mixed: true });
    expect(mixedText(libs)).toBe("nats.js 3.0.2 on 120, nats.js 2.29.3 on 24");
    expect(libraryCell(app())).toEqual({ text: "nats.js 3.0.2", mixed: false });
    expect(libraryCell(app({ libraries: [] }))).toEqual({ text: "", mixed: false });
  });

  it("names the pods in the sub-line only when the names give them", () => {
    expect(subLine(resizer())).toBe("4 connections, 4 pods");
    expect(subLine(app({ connections: 1 }))).toBe("1 connection");
  });

  it("links an application to its connections, tooling included for a tooling row, and (unnamed) nowhere", () => {
    expect(connectionsLink(app())).toBe("/connections?name=order-service");
    expect(connectionsLink(app({ name: "natslens", tooling: true }))).toBe("/connections?name=natslens&tooling=1");
    expect(connectionsLink(app({ name: "(unnamed)" }))).toBeUndefined();
  });

  it("links reconnects to the Events page only where the connections carry the application's own name", () => {
    expect(eventsLink(app({ name: "label printer" }))).toBe("/events?client=label%20printer");
    expect(eventsLink(resizer())).toBeUndefined();
    expect(eventsLink(app({ name: "(unnamed)" }))).toBeUndefined();
  });

  it("links reconnects to the unmuted client-004 finding before a muted one", () => {
    const f = dispatcher().findings[0];
    const muted = { ...f, id: "muted", muted: true };
    expect(reconnectFinding(dispatcher({ findings: [muted, f] }))?.id).toBe(f.id);
    expect(reconnectFinding(dispatcher({ findings: [muted] }))?.id).toBe("muted");
    expect(reconnectFinding(dispatcher({ findings: [{ ...f, checkCode: "client-003" }] }))).toBeUndefined();
  });

  it("sorts unset values last in both directions", () => {
    const rows = [app({ name: "a", rttMs: undefined }), app({ name: "b", rttMs: 2 }), app({ name: "c", rttMs: 1 })];
    expect(sortRows(rows, APP_SORT.rtt, "asc").map((r) => r.name)).toEqual(["c", "b", "a"]);
    expect(sortRows(rows, APP_SORT.rtt, "desc").map((r) => r.name)).toEqual(["b", "c", "a"]);
    const t = [app({ name: "x", traffic: hours(() => undefined) }), app({ name: "y" })];
    expect(sortRows(t, APP_SORT.traffic, "asc").map((r) => r.name)).toEqual(["y", "x"]);
  });

  it("names the sparkline with its hours, its totals, and how many hours were measured when not all", () => {
    expect(trafficLabel(resizer({ traffic: hours((i) => (i >= 18 ? [10, 5] : undefined)) }))).toBe(
      "resize-worker messages in and out, the 24 hours to 14:00 UTC: 60 in (5.86 KiB), 30 out (2.93 KiB); 6 of 24 hours measured",
    );
    expect(trafficLabel(app({ name: "x", traffic: hours(() => undefined) }))).toBe("x messages in and out, the 24 hours to 14:00 UTC: not measured");
    expect(trafficLabel(app({ name: "x", inMsgs: 1, outMsgs: 2, inBytes: 3, outBytes: 4 }))).toBe("x messages in and out, the 24 hours to 14:00 UTC: 1 in (3 B), 2 out (4 B)");
  });

  it("notes partial traffic only when no row has all 24 hours", () => {
    const six = hours((i) => (i >= 18 ? [1, 1] : undefined));
    expect(trafficNote([app({ traffic: six }), resizer({ traffic: six })])).toBe(
      "Traffic is measured for 6 of the 24 hours before this collection's hour; the hours without a measurement are gaps in the lines, not zeros.",
    );
    expect(trafficNote([app({ traffic: six }), resizer()])).toBeUndefined();
    expect(trafficNote([app({ traffic: hours(() => undefined) })])).toBe("No traffic measured in the 24 hours before this collection's hour: the hourly rollup holds none.");
    expect(trafficNote([])).toBeUndefined();
  });

  it("says why nothing is listed: only tooling, or no connection", () => {
    expect(emptyText({ toolingHidden: 2 }, false, "")).toBe("Only tooling connections in this collection: 2 connections. Include tooling to list them.");
    expect(emptyText({ toolingHidden: 0 }, false, "Shop")).toBe("No client connection in account Shop in this collection.");
    expect(emptyText({ toolingHidden: 2 }, true, "")).toBe("No client connection in this collection.");
  });
});

describe("TrafficSpark segments", () => {
  it("breaks the line at an hour without a measurement instead of drawing a zero", () => {
    const h = hours((i) => (i === 5 || i === 6 || i === 20 ? undefined : [i, 0]));
    const runs = segments(h, (x) => x.inMsgs, 23);
    expect(runs.map((r) => r.length)).toEqual([5, 13, 3]);
    // The highest value reaches the top margin; 0 lies on the baseline.
    expect(runs[2][2].y).toBeCloseTo(3);
    expect(runs[0][0].y).toBeCloseTo(22);
  });
});
