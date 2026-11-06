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

it("clips server intervals to the observation and chart, preserving short hatches",()=>{
 const h={start:100,end:500,live:true,gaps:[],serverGaps:[{start:490,end:700,reason:"Server did not answer"},{start:800,end:900,reason:"outside"}]};
 const {container}=render(<HistoryContext.Provider value={h}><Sparkline points={[]} status="ok" windows={[]} start={0} end={10000} label="CPU"/></HistoryContext.Provider>);
 const mark=screen.getByRole("img",{name:"Server did not answer"});
 expect(Number(mark.getAttribute("x"))+Number(mark.getAttribute("width"))).toBeLessThanOrEqual(10);
 expect(screen.queryByRole("img",{name:"outside"})).not.toBeInTheDocument();
 expect(container.querySelectorAll(".dh-chart-server-gap")).toHaveLength(1);
});
