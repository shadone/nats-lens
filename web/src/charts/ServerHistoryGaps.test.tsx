import { render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
import { HistoryContext } from "../components/HistoryNote";
import { Sparkline } from "./Sparkline";

it("hatches recorded server missing reports separately and breaks both metric traces", () => {
 const h = { start: 0, end: 1000, live: false, gaps: [], serverGaps: [{ start: 300, end: 600, reason: "Server did not answer" }] };
 const points = [{time:100,value:1},{time:700,value:2}];
 const {container}=render(<HistoryContext.Provider value={h}><Sparkline points={points} points2={points} status="ok" windows={[]} start={0} end={1000} label="CPU" /></HistoryContext.Provider>);
 expect(screen.getByRole("img",{name:/Server did not answer/})).toBeInTheDocument();
 expect(container.querySelectorAll("polyline")).toHaveLength(4);
 expect(screen.queryByRole("img",{name:/No collections/})).not.toBeInTheDocument();
});
